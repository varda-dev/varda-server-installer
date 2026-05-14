package serverinstaller

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func installServerConfig(targetDir string, manifest Manifest) error {
	if manifest.ServerConfig == nil || manifest.ServerConfig.URL == "" {
		return nil
	}

	if err := os.MkdirAll(cacheDir(targetDir), 0o755); err != nil {
		return err
	}

	cachePath := filepath.Join(cacheDir(targetDir), "server-config.zip")
	if err := downloadToFile(manifest.ServerConfig.URL, cachePath, true, "server config", DownloadChecks{SHA1: manifest.ServerConfig.SHA1}); err != nil {
		return err
	}

	return extractZipFile(cachePath, targetDir)
}

func extractZipFile(zipPath, targetDir string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	rootAbs, err := filepath.Abs(targetDir)
	if err != nil {
		return err
	}
	rootAbs = filepath.Clean(rootAbs)

	for _, entry := range reader.File {
		if err := extractZipEntry(entry, rootAbs); err != nil {
			return err
		}
	}

	return nil
}

func extractZipEntry(entry *zip.File, rootAbs string) error {
	name := entry.Name
	if name == "" {
		return nil
	}
	if strings.Contains(name, "\\") {
		return fmt.Errorf("zip entry uses backslash path separators: %s", name)
	}
	if path.IsAbs(name) {
		return fmt.Errorf("zip entry is absolute: %s", name)
	}
	if filepath.IsAbs(filepath.FromSlash(name)) {
		return fmt.Errorf("zip entry is absolute: %s", name)
	}

	cleaned := path.Clean(name)
	if cleaned == "." {
		return nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("zip entry escapes target dir: %s", name)
	}

	target := filepath.Join(rootAbs, filepath.FromSlash(cleaned))
	rel, err := filepath.Rel(rootAbs, target)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("zip entry escapes target dir: %s", name)
	}

	if entry.FileInfo().Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("zip entry is a symlink: %s", name)
	}

	if entry.FileInfo().IsDir() {
		return os.MkdirAll(target, 0o755)
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}

	src, err := entry.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	tmp, err := os.CreateTemp(filepath.Dir(target), filepath.Base(target)+".zip-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return replaceFile(tmpName, target)
}
