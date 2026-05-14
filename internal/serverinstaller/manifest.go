package serverinstaller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	defaultManifestURL             = "https://varda-dev.github.io/varda-modpack/manifest.json"
	supportedManifestSchemaVersion = 2
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
	Version      string `json:"version"`
	InstallerURL string `json:"installer_url"`
	SHA1         string `json:"sha1,omitempty"`
}

type ServerConfigManifest struct {
	URL  string `json:"url,omitempty"`
	SHA1 string `json:"sha1,omitempty"`
}

type ManifestMod struct {
	Name       string `json:"name,omitempty"`
	URL        string `json:"url"`
	WebsiteURL string `json:"website_url,omitempty"`
	SHA1       string `json:"sha1"`
	Size       int64  `json:"size,omitempty"`
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
	m.NeoForge.Version = strings.TrimSpace(m.NeoForge.Version)
	m.NeoForge.InstallerURL = strings.TrimSpace(m.NeoForge.InstallerURL)
	m.NeoForge.SHA1 = strings.TrimSpace(m.NeoForge.SHA1)
	if m.ServerConfig != nil {
		m.ServerConfig.URL = strings.TrimSpace(m.ServerConfig.URL)
		m.ServerConfig.SHA1 = strings.TrimSpace(m.ServerConfig.SHA1)
	}
	for i := range m.Mods {
		m.Mods[i].Name = strings.TrimSpace(m.Mods[i].Name)
		m.Mods[i].URL = strings.TrimSpace(m.Mods[i].URL)
		m.Mods[i].WebsiteURL = strings.TrimSpace(m.Mods[i].WebsiteURL)
		m.Mods[i].SHA1 = strings.TrimSpace(m.Mods[i].SHA1)
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
	if m.NeoForge.Version == "" {
		return fmt.Errorf("manifest neoforge.version must be non-empty")
	}
	if err := validateURLScheme(m.NeoForge.InstallerURL, "manifest neoforge.installer_url", "http", "https"); err != nil {
		return err
	}
	if m.NeoForge.SHA1 != "" {
		if err := validateSHA1Hex(m.NeoForge.SHA1, "manifest neoforge.sha1"); err != nil {
			return err
		}
	}

	if m.ServerConfig != nil && m.ServerConfig.URL != "" {
		if err := validateURLScheme(m.ServerConfig.URL, "manifest server_config.url", "http", "https", "file"); err != nil {
			return err
		}
	}
	if m.ServerConfig != nil && m.ServerConfig.SHA1 != "" {
		if err := validateSHA1Hex(m.ServerConfig.SHA1, "manifest server_config.sha1"); err != nil {
			return err
		}
	}

	for _, mod := range m.Mods {
		label := mod.Name
		filename, err := inferFilenameFromURL(mod.URL)
		if err != nil {
			if label == "" {
				label = mod.URL
			}
			return fmt.Errorf("%s: %w", label, err)
		}
		if label == "" {
			label = filename
		}
		if !isSafeModFilename(filename) {
			return fmt.Errorf("%s: unsafe or non-jar mod filename in manifest: %s", label, filename)
		}
		if err := validateURLScheme(mod.URL, "manifest mod url", "http", "https", "file"); err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		if mod.SHA1 == "" {
			return fmt.Errorf("%s: manifest mod sha1 must be non-empty", label)
		}
		if err := validateSHA1Hex(mod.SHA1, fmt.Sprintf("manifest mod sha1 for %s", label)); err != nil {
			return err
		}
		if mod.Size < 0 {
			return fmt.Errorf("%s: manifest mod size must be positive when present", label)
		}
		if mod.WebsiteURL != "" {
			if err := validateURLScheme(mod.WebsiteURL, "manifest mod website_url", "http", "https"); err != nil {
				return fmt.Errorf("%s: %w", label, err)
			}
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
