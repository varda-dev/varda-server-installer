package serverinstaller

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

var javaCommand = exec.Command

func InstallOrUpdateNeoForge(targetDir string, desired NeoForgeManifest, force bool) (string, error) {
	if desired.Version == "" {
		return "", fmt.Errorf("manifest neoforge.version must be non-empty")
	}
	if desired.InstallerURL == "" {
		return "", fmt.Errorf("manifest neoforge.installer_url must be non-empty")
	}

	actualVersion, err := inferNeoForgeVersionFromURL(desired.InstallerURL)
	if err != nil {
		return "", err
	}
	if actualVersion != desired.Version {
		return "", fmt.Errorf("manifest neoforge.version %q does not match installer URL version %q", desired.Version, actualVersion)
	}

	desiredDir := installedNeoForgeVersionDir(targetDir, desired.Version)
	installed, err := installedNeoForgeVersions(targetDir)
	if err != nil {
		return "", err
	}

	if dirExists(desiredDir) && !force {
		fmt.Printf("NeoForge %s already installed; skipping install.\n", desired.Version)
		if err := cleanupOldNeoForgeVersions(targetDir, desired.Version); err != nil {
			return "", err
		}
		return desired.Version, nil
	}

	switch {
	case len(installed) == 0:
		fmt.Printf("Installing NeoForge %s...\n", desired.Version)
	case force:
		fmt.Printf("Reinstalling NeoForge %s due to --force...\n", desired.Version)
	default:
		fmt.Printf("Updating NeoForge to %s...\n", desired.Version)
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
	if err := downloadToFile(desired.InstallerURL, installerPath, force, "NeoForge installer"); err != nil {
		return "", err
	}

	cmd := javaCommand("java", "-jar", installerPath, "--installServer")
	cmd.Dir = targetDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("NeoForge installer failed: %w\n%s", err, string(output))
	}

	if !dirExists(desiredDir) {
		return "", fmt.Errorf("NeoForge install completed but expected version directory is missing: %s", desired.Version)
	}

	if err := cleanupOldNeoForgeVersions(targetDir, desired.Version); err != nil {
		return "", err
	}

	return desired.Version, nil
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
