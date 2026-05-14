package serverinstaller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	manifestcontract "github.com/varda-dev/varda-server-installer/internal/manifest"
)

const (
	defaultManifestURL = "https://varda-dev.github.io/varda-modpack/manifest.json"
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

func (m Manifest) Validate() error {
	if m.SchemaVersion != manifestcontract.SupportedSchemaVersion {
		return fmt.Errorf("unsupported manifest schema_version %d", m.SchemaVersion)
	}
	if m.Pack != manifestcontract.ExpectedPack {
		return fmt.Errorf("unsupported pack %q", m.Pack)
	}
	if m.Version == "" {
		return fmt.Errorf("manifest version must be non-empty")
	}
	if m.Minecraft == "" {
		return fmt.Errorf("manifest minecraft must be non-empty")
	}
	if m.NeoForge.Version == "" {
		return fmt.Errorf("manifest neoforge.version must be non-empty")
	}
	if err := validateRequiredArtifactURL(m.NeoForge.InstallerURL, "manifest neoforge.installer_url", ".jar"); err != nil {
		return err
	}
	if m.NeoForge.SHA1 == "" {
		return fmt.Errorf("manifest neoforge.sha1 must be non-empty")
	}
	if err := validateSHA1Hex(m.NeoForge.SHA1, "manifest neoforge.sha1"); err != nil {
		return err
	}

	if m.ServerConfig == nil {
		return fmt.Errorf("manifest server_config must be non-empty")
	}
	if m.ServerConfig.URL == "" {
		return fmt.Errorf("manifest server_config.url must be non-empty")
	}
	if err := validateRequiredArtifactURL(m.ServerConfig.URL, "manifest server_config.url", ".zip"); err != nil {
		return err
	}
	if m.ServerConfig.SHA1 == "" {
		return fmt.Errorf("manifest server_config.sha1 must be non-empty")
	}
	if err := validateSHA1Hex(m.ServerConfig.SHA1, "manifest server_config.sha1"); err != nil {
		return err
	}
	if len(m.Mods) == 0 {
		return fmt.Errorf("manifest mods must be non-empty")
	}

	for _, mod := range m.Mods {
		spec, err := modSpecFromManifestMod(mod)
		if err != nil {
			return err
		}
		if mod.WebsiteURL != "" {
			label := mod.Name
			if label == "" {
				label = spec.FileName
			}
			if err := validateURLScheme(mod.WebsiteURL, "manifest mod website_url", "http", "https"); err != nil {
				return fmt.Errorf("%s: %w", label, err)
			}
		}
	}

	return nil
}

func validateRequiredArtifactURL(raw, label, suffix string) error {
	if raw == "" {
		return fmt.Errorf("%s must be non-empty", label)
	}
	if err := validateURLScheme(raw, label, "http", "https"); err != nil {
		return err
	}
	filename, err := inferFilenameFromURL(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if !strings.HasSuffix(strings.ToLower(filename), strings.ToLower(suffix)) {
		return fmt.Errorf("%s must end with %s: %s", label, suffix, filename)
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
