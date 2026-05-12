package serverinstaller

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	defaultManifestURL             = "https://varda-dev.github.io/varda-modpack/manifest.json"
	supportedManifestSchemaVersion = 1
)

type Manifest struct {
	SchemaVersion int                   `json:"schema_version"`
	Pack          string                `json:"pack"`
	Version       string                `json:"version"`
	Minecraft     string                `json:"minecraft,omitempty"`
	NeoForge      NeoForgeManifest      `json:"neoforge"`
	ServerConfig  *ServerConfigManifest `json:"server_config,omitempty"`
	Mods          []ManifestMod         `json:"mods,omitempty"`
}

type NeoForgeManifest struct {
	InstallerURL string `json:"installer_url"`
}

type ServerConfigManifest struct {
	URL    string `json:"url,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

type ManifestMod struct {
	URL string `json:"url"`
}

func LoadManifestFromBytes(data []byte) (Manifest, error) {
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	manifest.normalize()
	return manifest, nil
}

func (m *Manifest) normalize() {
	m.Pack = strings.TrimSpace(m.Pack)
	m.Version = strings.TrimSpace(m.Version)
	m.Minecraft = strings.TrimSpace(m.Minecraft)
	m.NeoForge.InstallerURL = strings.TrimSpace(m.NeoForge.InstallerURL)
	if m.ServerConfig != nil {
		m.ServerConfig.URL = strings.TrimSpace(m.ServerConfig.URL)
		m.ServerConfig.SHA256 = strings.TrimSpace(m.ServerConfig.SHA256)
	}
	for i := range m.Mods {
		m.Mods[i].URL = strings.TrimSpace(m.Mods[i].URL)
	}
}

func (m Manifest) packVersionString() string { return m.Version }

func (m Manifest) Validate() error {
	if m.SchemaVersion != supportedManifestSchemaVersion {
		return fmt.Errorf("unsupported manifest schema version: %d", m.SchemaVersion)
	}
	if m.Pack != "varda" {
		return fmt.Errorf("manifest pack must be varda")
	}
	if m.Version == "" {
		return fmt.Errorf("manifest version must be non-empty")
	}
	if err := validateURLScheme(m.NeoForge.InstallerURL, "manifest neoforge.installer_url", "http", "https"); err != nil {
		return err
	}
	if _, err := inferNeoForgeVersionFromURL(m.NeoForge.InstallerURL); err != nil {
		return err
	}

	if m.ServerConfig != nil && m.ServerConfig.URL != "" {
		if err := validateURLScheme(m.ServerConfig.URL, "manifest server_config.url", "http", "https", "file"); err != nil {
			return err
		}
	}
	if m.ServerConfig != nil && m.ServerConfig.SHA256 != "" {
		if len(m.ServerConfig.SHA256) != 64 {
			return fmt.Errorf("manifest server_config.sha256 must be 64 hex characters")
		}
		if _, err := hex.DecodeString(m.ServerConfig.SHA256); err != nil {
			return fmt.Errorf("manifest server_config.sha256 must be hex: %w", err)
		}
	}

	for _, mod := range m.Mods {
		filename, err := inferFilenameFromURL(mod.URL)
		if err != nil {
			return fmt.Errorf("%w for %s", err, mod.URL)
		}
		if !isSafeModFilename(filename) {
			return fmt.Errorf("unsafe or non-jar mod filename in manifest: %s", filename)
		}
		if err := validateURLScheme(mod.URL, "manifest mod url", "http", "https", "file"); err != nil {
			return fmt.Errorf("%s for %s", err, filename)
		}
	}

	return nil
}

func validateURLScheme(raw, label string, allowed ...string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if parsed.Scheme == "" {
		return fmt.Errorf("%s must use a URL scheme: %s", label, raw)
	}

	for _, scheme := range allowed {
		if parsed.Scheme != scheme {
			continue
		}
		switch scheme {
		case "http", "https":
			if parsed.Host == "" {
				return fmt.Errorf("%s must include a host: %s", label, raw)
			}
		case "file":
			if parsed.Path == "" && parsed.Host == "" {
				return fmt.Errorf("%s must include a file path: %s", label, raw)
			}
		}
		return nil
	}

	return fmt.Errorf("%s must use %s: %s", label, strings.Join(allowed, ", "), raw)
}
