package serverinstaller

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

var javaCommand = exec.Command
var downloadFile = downloadToFile

func InstallOrUpdateNeoForge(targetDir string, desired NeoForgeManifest, force bool) (string, error) {
	if desired.InstallerURL == "" {
		return "", fmt.Errorf("manifest neoforge.installer_url must be non-empty")
	}

	desiredVersion, err := inferNeoForgeVersionFromURL(desired.InstallerURL)
	if err != nil {
		return "", err
	}

	desiredDir := installedNeoForgeVersionDir(targetDir, desiredVersion)
	installed, err := installedNeoForgeVersions(targetDir)
	if err != nil {
		return "", err
	}

	if dirExists(desiredDir) && !force {
		fmt.Printf("NeoForge %s already installed; skipping install.\n", desiredVersion)
		if err := cleanupOldNeoForgeVersions(targetDir, desiredVersion); err != nil {
			return "", err
		}
		return desiredVersion, nil
	}

	switch {
	case len(installed) == 0:
		fmt.Printf("Installing NeoForge %s...\n", desiredVersion)
	case force:
		fmt.Printf("Reinstalling NeoForge %s due to --force...\n", desiredVersion)
	default:
		fmt.Printf("Updating NeoForge to %s...\n", desiredVersion)
	}

	tempDir, err := os.MkdirTemp("", "varda-neoforge-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tempDir)

	installerName, err := inferFilenameFromURL(desired.InstallerURL)
	if err != nil {
		return "", err
	}
	installerPath := filepath.Join(tempDir, installerName)
	var checks []DownloadChecks
	if desired.SHA1URL != "" {
		rawSHA1, err := downloadURLToBytes(desired.SHA1URL)
		if err != nil {
			return "", fmt.Errorf("download NeoForge installer sha1: %w", err)
		}
		expectedSHA1, err := parseSHA1Digest(rawSHA1, "NeoForge installer sha1")
		if err != nil {
			return "", err
		}
		checks = append(checks, DownloadChecks{SHA1: expectedSHA1})
	}
	if err := downloadFile(desired.InstallerURL, installerPath, force, "NeoForge installer", checks...); err != nil {
		return "", err
	}

	cmd := javaCommand("java", "-jar", installerPath, "--installServer")
	cmd.Dir = targetDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("NeoForge installer failed: %w\n%s", err, string(output))
	}

	if !dirExists(desiredDir) {
		return "", fmt.Errorf("NeoForge install completed but expected version directory is missing: %s", desiredVersion)
	}

	if err := cleanupOldNeoForgeVersions(targetDir, desiredVersion); err != nil {
		return "", err
	}

	return desiredVersion, nil
}

func cleanupNeoForgeInstallerArtifacts(targetDir string) error {
	patterns := []string{
		filepath.Join(targetDir, "neoforge-*-installer.jar"),
		filepath.Join(targetDir, "neoforge-*-installer.jar.log"),
		filepath.Join(targetDir, "installer.log"),
	}

	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return err
		}

		for _, match := range matches {
			if err := os.Remove(match); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}

	return nil
}

func installedNeoForgeVersions(targetDir string) ([]string, error) {
	root := desiredNeoForgeRoot(targetDir)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var versions []string
	for _, entry := range entries {
		if entry.IsDir() {
			versions = append(versions, entry.Name())
		}
	}
	sort.Strings(versions)
	return versions, nil
}

func cleanupOldNeoForgeVersions(targetDir, desired string) error {
	root := desiredNeoForgeRoot(targetDir)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == desired {
			continue
		}
		oldPath := filepath.Join(root, entry.Name())
		fmt.Printf("Removing old NeoForge version: %s\n", entry.Name())
		if err := os.RemoveAll(oldPath); err != nil {
			return err
		}
	}

	return nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
