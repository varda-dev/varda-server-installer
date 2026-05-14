package serverinstaller

import (
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"
)

func targetPath(targetDir string, parts ...string) string {
	items := append([]string{targetDir}, parts...)
	return filepath.Join(items...)
}

func desiredNeoForgeRoot(targetDir string) string {
	return targetPath(targetDir, "libraries", "net", "neoforged", "neoforge")
}

func installedNeoForgeVersionDir(targetDir, version string) string {
	return filepath.Join(desiredNeoForgeRoot(targetDir), version)
}

func modsDir(targetDir string) string {
	return targetPath(targetDir, "mods")
}

func isSafeModFilename(name string) bool {
	if name == "" || filepath.IsAbs(name) {
		return false
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return false
	}
	if strings.Contains(name, "..") {
		return false
	}
	return strings.HasSuffix(strings.ToLower(name), ".jar")
}

func inferFilenameFromURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" && parsed.Scheme != "file" {
		return "", fmt.Errorf("unsupported URL scheme: %s", parsed.Scheme)
	}
	if (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host == "" {
		return "", fmt.Errorf("URL must include a host: %s", raw)
	}
	name := path.Base(parsed.EscapedPath())
	if name == "." || name == "/" || name == "" {
		return "", fmt.Errorf("URL does not contain a file name: %s", raw)
	}
	return url.PathUnescape(name)
}
