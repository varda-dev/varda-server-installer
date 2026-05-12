package serverinstaller

import (
	"fmt"
	"os"
)

func packVersionPath(targetDir string) string {
	return vardaFile(targetDir, "pack-version.txt")
}

func installerVersionPath(targetDir string) string {
	return vardaFile(targetDir, "installer-version.txt")
}

func cacheDir(targetDir string) string {
	return targetPath(targetDir, ".varda", "cache")
}

func vardaDir(targetDir string) string {
	return targetPath(targetDir, ".varda")
}

func vardaFile(targetDir, name string) string {
	return targetPath(vardaDir(targetDir), name)
}

func WriteInstallDiagnostics(targetDir string, manifest Manifest) error {
	if err := os.MkdirAll(vardaDir(targetDir), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(packVersionPath(targetDir), []byte(manifest.packVersionString()+"\n"), 0o644); err != nil {
		return fmt.Errorf("write pack version: %w", err)
	}
	if err := os.WriteFile(installerVersionPath(targetDir), []byte(Version+"\n"), 0o644); err != nil {
		return fmt.Errorf("write installer version: %w", err)
	}
	return nil
}

func FetchRemoteManifest(rawURL string) (Manifest, error) {
	raw, err := downloadURLToBytes(rawURL)
	if err != nil {
		return Manifest{}, fmt.Errorf("fetch manifest: %w", err)
	}

	manifest, err := LoadManifestFromBytes(raw)
	if err != nil {
		return Manifest{}, err
	}

	return manifest, nil
}

func manifestCheckSummary(manifest Manifest) string {
	return fmt.Sprintf(
		"Manifest check:\n  pack:           %s\n  version:        %s\n  minecraft:      %s\n  NeoForge:       %s\n  mods:           %d\n  server config:  %s\n",
		manifest.Pack,
		manifest.packVersionString(),
		manifest.Minecraft,
		manifest.NeoForge.Version,
		len(manifest.Mods),
		serverConfigSummary(manifest.ServerConfig),
	)
}

func serverConfigSummary(cfg *ServerConfigManifest) string {
	if cfg != nil && cfg.URL != "" {
		return "present"
	}
	return "absent"
}
