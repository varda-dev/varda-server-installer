package serverinstaller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	manifestcontract "github.com/varda-dev/varda-server-installer/internal/manifest"
)

func TestManifestContractFixtureUsesGeneratedConstants(t *testing.T) {
	t.Parallel()

	manifest := loadValidManifestFixture(t)
	if manifestcontract.ManifestSchemaURL != "https://varda-dev.github.io/varda-manifest/manifest.schema.json" {
		t.Fatalf("ManifestSchemaURL = %q", manifestcontract.ManifestSchemaURL)
	}
	if manifest.SchemaVersion != manifestcontract.SupportedSchemaVersion {
		t.Fatalf("schema_version = %d, want %d", manifest.SchemaVersion, manifestcontract.SupportedSchemaVersion)
	}
	if manifest.Pack != manifestcontract.ExpectedPack {
		t.Fatalf("pack = %q, want %q", manifest.Pack, manifestcontract.ExpectedPack)
	}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestManifestValidateRejectsMissingRequiredRuntimeFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		mutate  func(*Manifest)
		wantErr string
	}{
		{
			name: "version",
			mutate: func(m *Manifest) {
				m.Version = ""
			},
			wantErr: "version",
		},
		{
			name: "minecraft",
			mutate: func(m *Manifest) {
				m.Minecraft = ""
			},
			wantErr: "minecraft",
		},
		{
			name: "neoforge_sha1",
			mutate: func(m *Manifest) {
				m.NeoForge.SHA1 = ""
			},
			wantErr: "neoforge.sha1",
		},
		{
			name: "server_config",
			mutate: func(m *Manifest) {
				m.ServerConfig = nil
			},
			wantErr: "server_config",
		},
		{
			name: "mods",
			mutate: func(m *Manifest) {
				m.Mods = nil
			},
			wantErr: "mods",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			manifest := loadValidManifestFixture(t)
			tc.mutate(&manifest)

			err := manifest.Validate()
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), tc.wantErr) {
				t.Fatalf("Validate() error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func loadValidManifestFixture(t *testing.T) Manifest {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "manifest", "testdata", "valid", "manifest.v2.json"))
	if err != nil {
		t.Fatal(err)
	}

	manifest, err := LoadManifestFromBytes(raw)
	if err != nil {
		t.Fatal(err)
	}

	return manifest
}
