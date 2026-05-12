package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const versionSymbol = "github.com/varda-dev/varda-server-installer/internal/serverinstaller.Version"

var versionPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type targetSpec struct {
	GOOS          string
	GOARCH        string
	BinaryName    string
	ArchiveName   string
	ArchiveFormat string
}

type options struct {
	Help    bool
	Version string
	OutDir  string
	Force   bool
	Clean   bool
	Verbose bool
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}

	repoRoot, err := findRepoRoot()
	if err != nil {
		return err
	}

	outDir, err := resolvePath(repoRoot, opts.OutDir)
	if err != nil {
		return err
	}

	if opts.Clean {
		if err := os.RemoveAll(outDir); err != nil {
			return fmt.Errorf("clean output dir: %w", err)
		}
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	targets := releaseTargets(opts.Version)
	for _, target := range targets {
		finalPath := filepath.Join(outDir, target.ArchiveName)
		if exists(finalPath) && !opts.Force && !opts.Clean {
			return fmt.Errorf("output archive already exists: %s; use -force or -clean", finalPath)
		}
	}

	stagingDir, err := os.MkdirTemp("", "varda-server-installer-release-*")
	if err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}
	defer os.RemoveAll(stagingDir)

	var createdArchives []string
	for _, target := range targets {
		archivePath, err := buildTarget(repoRoot, stagingDir, target, opts)
		if err != nil {
			return err
		}

		finalPath := filepath.Join(outDir, target.ArchiveName)
		if err := writeFinalArchive(archivePath, finalPath, opts.Force, opts.Clean); err != nil {
			return err
		}
		createdArchives = append(createdArchives, finalPath)
	}

	checksumsPath, err := writeChecksums(outDir, createdArchives)
	if err != nil {
		return err
	}

	fmt.Println("Created archives:")
	for _, path := range createdArchives {
		fmt.Println(path)
	}
	fmt.Println(checksumsPath)
	return nil
}

func parseOptions(args []string) (options, error) {
	var opts options
	fs := flag.NewFlagSet("build-release", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {
		fmt.Fprintln(os.Stdout, "Usage: go tool build-release -v VERSION [options]")
		fmt.Fprintln(os.Stdout)
		fmt.Fprintln(os.Stdout, "Build Varda server installer release archives.")
		fmt.Fprintln(os.Stdout)
		fmt.Fprintln(os.Stdout, "Options:")
		fmt.Fprintln(os.Stdout, "  -v, --version VERSION")
		fmt.Fprintln(os.Stdout, "        release version, example: 0.1.4")
		fmt.Fprintln(os.Stdout, "  -o, --out DIR")
		fmt.Fprintln(os.Stdout, "        output directory (default tmp/release)")
		fmt.Fprintln(os.Stdout, "  -f, --force")
		fmt.Fprintln(os.Stdout, "        overwrite existing artifacts")
		fmt.Fprintln(os.Stdout, "  -c, --clean")
		fmt.Fprintln(os.Stdout, "        remove output directory before building")
		fmt.Fprintln(os.Stdout, "  --verbose")
		fmt.Fprintln(os.Stdout, "        print commands and Go output")
		fmt.Fprintln(os.Stdout, "  -h, --help")
		fmt.Fprintln(os.Stdout, "        print help")
	}
	fs.BoolVar(&opts.Help, "help", false, "print help")
	fs.BoolVar(&opts.Help, "h", false, "print help")
	fs.StringVar(&opts.Version, "version", "", "release version")
	fs.StringVar(&opts.Version, "v", "", "release version")
	fs.StringVar(&opts.OutDir, "out", "tmp/release", "output directory")
	fs.StringVar(&opts.OutDir, "o", "tmp/release", "output directory")
	fs.BoolVar(&opts.Force, "force", false, "overwrite existing artifacts")
	fs.BoolVar(&opts.Force, "f", false, "overwrite existing artifacts")
	fs.BoolVar(&opts.Clean, "clean", false, "remove output directory before building")
	fs.BoolVar(&opts.Clean, "c", false, "remove output directory before building")
	fs.BoolVar(&opts.Verbose, "verbose", false, "print commands and Go output")

	normalized := normalizeArgs(args)
	if err := fs.Parse(normalized); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return opts, nil
		}
		return opts, err
	}

	if opts.Help {
		fs.Usage()
		return opts, flag.ErrHelp
	}
	if opts.Version == "" {
		return opts, errors.New("-version is required")
	}
	if !versionPattern.MatchString(opts.Version) {
		return opts, fmt.Errorf("invalid version %q: use letters, numbers, dots, underscores, and hyphens only", opts.Version)
	}
	return opts, nil
}

func normalizeArgs(args []string) []string {
	normalized := make([]string, len(args))
	for i, arg := range args {
		switch arg {
		case "--help":
			normalized[i] = "-help"
		case "--version":
			normalized[i] = "-version"
		case "--out":
			normalized[i] = "-out"
		case "--force":
			normalized[i] = "-force"
		case "--clean":
			normalized[i] = "-clean"
		case "--verbose":
			normalized[i] = "-verbose"
		default:
			switch {
			case strings.HasPrefix(arg, "--help="):
				normalized[i] = "-help" + strings.TrimPrefix(arg, "--help")
			case strings.HasPrefix(arg, "--version="):
				normalized[i] = "-version" + strings.TrimPrefix(arg, "--version")
			case strings.HasPrefix(arg, "--out="):
				normalized[i] = "-out" + strings.TrimPrefix(arg, "--out")
			case strings.HasPrefix(arg, "--force="):
				normalized[i] = "-force" + strings.TrimPrefix(arg, "--force")
			case strings.HasPrefix(arg, "--clean="):
				normalized[i] = "-clean" + strings.TrimPrefix(arg, "--clean")
			case strings.HasPrefix(arg, "--verbose="):
				normalized[i] = "-verbose" + strings.TrimPrefix(arg, "--verbose")
			default:
				normalized[i] = arg
			}
		}
	}
	return normalized
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if exists(filepath.Join(dir, "go.mod")) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("could not locate repo root containing go.mod")
		}
		dir = parent
	}
}

func resolvePath(root, raw string) (string, error) {
	if filepath.IsAbs(raw) {
		return filepath.Clean(raw), nil
	}
	return filepath.Clean(filepath.Join(root, raw)), nil
}

func releaseTargets(version string) []targetSpec {
	return []targetSpec{
		{
			GOOS:          "windows",
			GOARCH:        "amd64",
			BinaryName:    "varda-server-installer.exe",
			ArchiveName:   fmt.Sprintf("varda-server-installer-%s-windows-amd64.zip", version),
			ArchiveFormat: "zip",
		},
		{
			GOOS:          "linux",
			GOARCH:        "amd64",
			BinaryName:    "varda-server-installer",
			ArchiveName:   fmt.Sprintf("varda-server-installer-%s-linux-amd64.tar.gz", version),
			ArchiveFormat: "tar.gz",
		},
		{
			GOOS:          "linux",
			GOARCH:        "arm64",
			BinaryName:    "varda-server-installer",
			ArchiveName:   fmt.Sprintf("varda-server-installer-%s-linux-arm64.tar.gz", version),
			ArchiveFormat: "tar.gz",
		},
		{
			GOOS:          "darwin",
			GOARCH:        "amd64",
			BinaryName:    "varda-server-installer",
			ArchiveName:   fmt.Sprintf("varda-server-installer-%s-darwin-amd64.tar.gz", version),
			ArchiveFormat: "tar.gz",
		},
		{
			GOOS:          "darwin",
			GOARCH:        "arm64",
			BinaryName:    "varda-server-installer",
			ArchiveName:   fmt.Sprintf("varda-server-installer-%s-darwin-arm64.tar.gz", version),
			ArchiveFormat: "tar.gz",
		},
	}
}

func buildTarget(repoRoot, stagingRoot string, target targetSpec, opts options) (string, error) {
	stageDir := filepath.Join(stagingRoot, target.GOOS+"-"+target.GOARCH)
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return "", fmt.Errorf("create staging dir: %w", err)
	}

	binaryPath := filepath.Join(stageDir, target.BinaryName)
	args := []string{
		"build",
		"-trimpath",
		"-buildvcs=false",
		"-ldflags",
		fmt.Sprintf("-s -w -X %s=%s", versionSymbol, opts.Version),
		"-o",
		binaryPath,
		"./cmd/varda-server-installer",
	}

	cmd := exec.Command("go", args...)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS="+target.GOOS,
		"GOARCH="+target.GOARCH,
	)

	if opts.Verbose {
		fmt.Printf("go %s\n", strings.Join(args, " "))
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		if len(output) > 0 {
			os.Stderr.Write(output)
			if output[len(output)-1] != '\n' {
				os.Stderr.WriteString("\n")
			}
		}
		return "", fmt.Errorf("build %s/%s failed", target.GOOS, target.GOARCH)
	}
	if opts.Verbose && len(output) > 0 {
		os.Stdout.Write(output)
	}

	archivePath := filepath.Join(stageDir, target.ArchiveName)
	files := []archiveItem{
		{source: binaryPath, name: target.BinaryName, mode: 0o755},
	}

	switch target.ArchiveFormat {
	case "zip":
		if err := createZipArchive(archivePath, files); err != nil {
			return "", err
		}
	case "tar.gz":
		if err := createTarGzArchive(archivePath, files); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("unsupported archive format: %s", target.ArchiveFormat)
	}

	if err := os.Remove(binaryPath); err != nil {
		return "", fmt.Errorf("remove staged binary: %w", err)
	}

	return archivePath, nil
}

type archiveItem struct {
	source string
	name   string
	mode   int64
}

func createZipArchive(archivePath string, items []archiveItem) error {
	file, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	zw := zip.NewWriter(file)

	const fixedUnix = 315532800 // 1980-01-01 UTC
	fixedTime := time.Unix(fixedUnix, 0).UTC()

	for _, item := range items {
		data, err := os.ReadFile(item.source)
		if err != nil {
			return err
		}

		header, err := zip.FileInfoHeader(fileInfoForArchive(item))
		if err != nil {
			return err
		}
		header.Name = item.name
		header.Method = zip.Deflate
		header.SetModTime(fixedTime)

		writer, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		if _, err := writer.Write(data); err != nil {
			return err
		}
	}

	return zw.Close()
}

func createTarGzArchive(archivePath string, items []archiveItem) error {
	file, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	gz, err := gzip.NewWriterLevel(file, gzip.BestCompression)
	if err != nil {
		return err
	}
	gz.Header.ModTime = time.Unix(0, 0).UTC()
	gz.Header.OS = 255

	tw := tar.NewWriter(gz)

	fixedTime := time.Unix(0, 0).UTC()
	for _, item := range items {
		data, err := os.ReadFile(item.source)
		if err != nil {
			return err
		}

		header := &tar.Header{
			Name:     item.name,
			Mode:     item.mode,
			Size:     int64(len(data)),
			ModTime:  fixedTime,
			Typeflag: tar.TypeReg,
			Uid:      0,
			Gid:      0,
			Uname:    "",
			Gname:    "",
			Format:   tar.FormatPAX,
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
	}

	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func fileInfoForArchive(item archiveItem) os.FileInfo {
	info, err := os.Stat(item.source)
	if err != nil {
		return fakeFileInfo{name: item.name, size: 0, mode: os.FileMode(item.mode)}
	}
	return archiveFileInfo{FileInfo: info, mode: os.FileMode(item.mode)}
}

type fakeFileInfo struct {
	name string
	size int64
	mode os.FileMode
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return f.size }
func (f fakeFileInfo) Mode() os.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Unix(0, 0) }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() any           { return nil }

type archiveFileInfo struct {
	os.FileInfo
	mode os.FileMode
}

func (f archiveFileInfo) Mode() os.FileMode {
	return (f.FileInfo.Mode() &^ os.ModePerm) | f.mode
}

func writeFinalArchive(stagedArchive, finalPath string, force, clean bool) error {
	if force || clean {
		if err := os.Remove(finalPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove existing archive %s: %w", finalPath, err)
		}
	}

	tmp, err := os.CreateTemp(filepath.Dir(finalPath), filepath.Base(finalPath)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	src, err := os.Open(stagedArchive)
	if err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}

	if _, err := io.Copy(tmp, src); err != nil {
		src.Close()
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	src.Close()
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		os.Remove(tmpPath)
		return err
	}

	return nil
}

func writeChecksums(outDir string, archives []string) (string, error) {
	sort.Slice(archives, func(i, j int) bool {
		return filepath.Base(archives[i]) < filepath.Base(archives[j])
	})
	var lines bytes.Buffer
	for _, archive := range archives {
		sum, err := sha256File(archive)
		if err != nil {
			return "", err
		}
		lines.WriteString(sum)
		lines.WriteString("  ")
		lines.WriteString(filepath.Base(archive))
		lines.WriteByte('\n')
	}

	path := filepath.Join(outDir, "checksums.txt")
	tmp, err := os.CreateTemp(outDir, "checksums.txt.tmp-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(lines.Bytes()); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return "", err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		os.Remove(tmpPath)
		return "", err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return "", err
	}
	return path, nil
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
