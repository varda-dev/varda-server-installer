// Package serverinstaller provides the Varda Minecraft server installer and updater.
package serverinstaller

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

type Options struct {
	Force           bool
	DownloadWorkers int
	TargetDir       string
	ManifestURL     string
	CheckManifest   bool
	VersionOnly     bool
	Help            bool
}

func Run(args []string) error {
	opts, err := parseOptions(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if opts.VersionOnly {
		fmt.Println(Version)
		return nil
	}

	manifest, err := FetchRemoteManifest(opts.ManifestURL)
	if err != nil {
		return err
	}
	manifest.normalize()
	if err := manifest.Validate(); err != nil {
		return err
	}

	if opts.CheckManifest {
		fmt.Print(manifestCheckSummary(manifest))
		fmt.Println("Check complete. No files changed.")
		return nil
	}

	if err := RequireJava21(); err != nil {
		return err
	}

	if opts.TargetDir == "" {
		opts.TargetDir = "."
	}

	if err := os.MkdirAll(opts.TargetDir, 0o755); err != nil {
		return fmt.Errorf("create target dir: %w", err)
	}

	if err := installServerConfig(opts.TargetDir, manifest); err != nil {
		return err
	}
	desiredNeoForgeVersion, err := InstallOrUpdateNeoForge(opts.TargetDir, manifest.NeoForge, opts.Force)
	if err != nil {
		return err
	}

	if err := WriteJvmArgs(opts.TargetDir, opts.Force); err != nil {
		return err
	}
	if err := PatchLaunchers(opts.TargetDir, desiredNeoForgeVersion); err != nil {
		return err
	}
	if err := cleanupNeoForgeInstallerArtifacts(opts.TargetDir); err != nil {
		return err
	}

	if err := ReconcileMods(opts.TargetDir, manifest.Mods, opts.Force, opts.DownloadWorkers); err != nil {
		return err
	}

	if err := WriteInstallDiagnostics(opts.TargetDir, manifest); err != nil {
		return err
	}

	fmt.Println("Setup complete.")
	return nil
}

func parseOptions(args []string) (Options, error) {
	var opts Options

	fs := flag.NewFlagSet("varda-server-installer", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {
		printInstallerUsage(os.Stdout)
	}
	fs.BoolVar(&opts.Help, "help", false, "print help")
	fs.BoolVar(&opts.Help, "h", false, "print help")
	fs.BoolVar(&opts.VersionOnly, "version", false, "print installer version and exit")
	fs.BoolVar(&opts.VersionOnly, "v", false, "print installer version and exit")
	fs.BoolVar(&opts.CheckManifest, "check-manifest", false, "validate the remote manifest and print a summary without changing files")
	fs.BoolVar(&opts.CheckManifest, "check", false, "validate the remote manifest and print a summary without changing files")
	fs.StringVar(&opts.TargetDir, "dir", ".", "server install directory")
	fs.StringVar(&opts.TargetDir, "d", ".", "server install directory")
	fs.BoolVar(&opts.Force, "force", false, "re-download/reinstall files")
	fs.BoolVar(&opts.Force, "f", false, "re-download/reinstall files")
	fs.StringVar(&opts.ManifestURL, "manifest-url", defaultManifestURL, "remote manifest URL")
	fs.IntVar(&opts.DownloadWorkers, "download-workers", 6, "mod download worker count")

	if err := fs.Parse(normalizeInstallerArgs(args)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.Usage()
			return opts, flag.ErrHelp
		}
		return opts, err
	}

	if opts.Help {
		fs.Usage()
		return opts, flag.ErrHelp
	}

	if opts.DownloadWorkers < 1 || opts.DownloadWorkers > 16 {
		return opts, fmt.Errorf("--download-workers must be between 1 and 16")
	}

	return opts, nil
}

func normalizeInstallerArgs(args []string) []string {
	normalized := make([]string, len(args))
	for i, arg := range args {
		switch arg {
		case "--help":
			normalized[i] = "-help"
		case "--version":
			normalized[i] = "-version"
		case "--check-manifest":
			normalized[i] = "-check-manifest"
		case "--check":
			normalized[i] = "-check"
		case "--dir":
			normalized[i] = "-dir"
		case "--force":
			normalized[i] = "-force"
		case "--manifest-url":
			normalized[i] = "-manifest-url"
		case "--download-workers":
			normalized[i] = "-download-workers"
		default:
			switch {
			case strings.HasPrefix(arg, "--help="):
				normalized[i] = "-help" + strings.TrimPrefix(arg, "--help")
			case strings.HasPrefix(arg, "--version="):
				normalized[i] = "-version" + strings.TrimPrefix(arg, "--version")
			case strings.HasPrefix(arg, "--check-manifest="):
				normalized[i] = "-check-manifest" + strings.TrimPrefix(arg, "--check-manifest")
			case strings.HasPrefix(arg, "--check="):
				normalized[i] = "-check" + strings.TrimPrefix(arg, "--check")
			case strings.HasPrefix(arg, "--dir="):
				normalized[i] = "-dir" + strings.TrimPrefix(arg, "--dir")
			case strings.HasPrefix(arg, "--force="):
				normalized[i] = "-force" + strings.TrimPrefix(arg, "--force")
			case strings.HasPrefix(arg, "--manifest-url="):
				normalized[i] = "-manifest-url" + strings.TrimPrefix(arg, "--manifest-url")
			case strings.HasPrefix(arg, "--download-workers="):
				normalized[i] = "-download-workers" + strings.TrimPrefix(arg, "--download-workers")
			default:
				normalized[i] = arg
			}
		}
	}
	return normalized
}

func printInstallerUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: varda-server-installer [options]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Install or update a Varda Minecraft server from the remote manifest.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options:")
	fmt.Fprintln(w, "  -h, --help                 Show this help text and exit")
	fmt.Fprintln(w, "  -v, --version              Print installer version and exit")
	fmt.Fprintln(w, "      --check-manifest       Validate remote manifest and print summary without changing files")
	fmt.Fprintln(w, "  -d, --dir DIR              Server install directory (default: .)")
	fmt.Fprintln(w, "  -f, --force                Re-download/reinstall files instead of keeping existing files")
	fmt.Fprintln(w, "      --manifest-url URL     Remote manifest URL")
	fmt.Fprintln(w, "      --download-workers N   Concurrent mod download workers (default: 6, range: 1-16)")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Default manifest URL:")
	fmt.Fprintln(w, "  https://varda-dev.github.io/varda-modpack/manifest.json")
}
