package serverinstaller

import (
	"archive/zip"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseJavaVersion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		output  string
		want    int
		wantErr bool
	}{
		{
			name:   "modern",
			output: `openjdk version "21.0.4" 2024-07-16`,
			want:   21,
		},
		{
			name:   "legacy",
			output: `java version "1.8.0_402"`,
			want:   8,
		},
		{
			name:    "invalid",
			output:  `java version`,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseJavaVersion(tc.output)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseJavaVersion() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("ParseJavaVersion() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestInferNeoForgeVersionFromURL(t *testing.T) {
	t.Parallel()

	got, err := inferNeoForgeVersionFromURL("https://maven.neoforged.net/releases/net/neoforged/neoforge/21.1.83/neoforge-21.1.83-installer.jar")
	if err != nil {
		t.Fatalf("inferNeoForgeVersionFromURL() error = %v", err)
	}
	if got != "21.1.83" {
		t.Fatalf("inferNeoForgeVersionFromURL() = %q, want %q", got, "21.1.83")
	}
}

func TestLoadManifestFromBytes(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
  "schema_version": 1,
  "pack": "varda",
  "version": "0.1.4",
  "minecraft": "1.21.1",
  "neoforge": {
    "version": "21.1.228",
    "installer_url": "https://maven.neoforged.net/releases/net/neoforged/neoforge/21.1.228/neoforge-21.1.228-installer.jar"
  },
  "server_config": {
    "url": "https://example.invalid/server-config.zip",
    "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  },
  "mods": [
    {
      "filename": "example.jar",
      "url": "https://example.invalid/example.jar"
    }
  ]
}`)

	manifest, err := LoadManifestFromBytes(raw)
	if err != nil {
		t.Fatalf("LoadManifestFromBytes() error = %v", err)
	}
	if manifest.Pack != "varda" || manifest.Version != "0.1.4" || manifest.NeoForge.Version != "21.1.228" {
		t.Fatalf("LoadManifestFromBytes() = %#v", manifest)
	}
	if got := manifest.packVersionString(); got != "0.1.4" {
		t.Fatalf("packVersionString() = %q, want %q", got, "0.1.4")
	}
}

func TestManifestValidateRejectsNeoForgeVersionMismatch(t *testing.T) {
	t.Parallel()

	manifest := validTestManifest()
	manifest.NeoForge.Version = "21.1.227"

	if err := manifest.Validate(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "does not match") {
		t.Fatalf("Validate() error = %v, want NeoForge version mismatch", err)
	}
}

func TestManifestValidateRejectsMalformedNeoForgeURL(t *testing.T) {
	t.Parallel()

	manifest := validTestManifest()
	manifest.NeoForge.InstallerURL = "https://"

	if err := manifest.Validate(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "host") {
		t.Fatalf("Validate() error = %v, want host rejection", err)
	}
}

func TestManifestValidateRejectsMissingNeoForgeHost(t *testing.T) {
	t.Parallel()

	manifest := validTestManifest()
	manifest.NeoForge.InstallerURL = "https:///neoforge-21.1.228-installer.jar"

	if err := manifest.Validate(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "host") {
		t.Fatalf("Validate() error = %v, want missing host rejection", err)
	}
}

func TestManifestValidateRejectsWrongPack(t *testing.T) {
	t.Parallel()

	manifest := Manifest{
		SchemaVersion: 1,
		Pack:          "other",
		Version:       "1.0.0",
		NeoForge: NeoForgeManifest{
			Version:      "21.1.1",
			InstallerURL: "https://maven.neoforged.net/releases/net/neoforged/neoforge/21.1.1/neoforge-21.1.1-installer.jar",
		},
	}

	if err := manifest.Validate(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "pack") {
		t.Fatalf("Validate() error = %v, want pack rejection", err)
	}
}

func TestManifestValidateRejectsUnsafeModFilename(t *testing.T) {
	t.Parallel()

	manifest := validTestManifest()
	manifest.Mods = []ManifestMod{{Filename: "../evil.jar", URL: "https://example.invalid/evil.jar"}}

	if err := manifest.Validate(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "unsafe") {
		t.Fatalf("Validate() error = %v, want unsafe filename rejection", err)
	}
}

func TestManifestValidateRejectsNonJarModFilename(t *testing.T) {
	t.Parallel()

	manifest := validTestManifest()
	manifest.Mods = []ManifestMod{{Filename: "example.zip", URL: "https://example.invalid/example.zip"}}

	if err := manifest.Validate(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "non-jar") {
		t.Fatalf("Validate() error = %v, want non-jar rejection", err)
	}
}

func TestWriteInstallDiagnosticsWritesOnlyDiagnostics(t *testing.T) {
	root := t.TempDir()
	oldVersion := Version
	Version = "test-123"
	defer func() { Version = oldVersion }()

	manifest := validTestManifest()
	if err := WriteInstallDiagnostics(root, manifest); err != nil {
		t.Fatalf("WriteInstallDiagnostics() error = %v", err)
	}

	for _, path := range []string{
		filepath.Join(root, ".varda", "pack-version.txt"),
		filepath.Join(root, ".varda", "installer-version.txt"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected diagnostic file %s: %v", path, err)
		}
	}
	for _, path := range []string{
		filepath.Join(root, ".varda", "manifest.json"),
		filepath.Join(root, ".varda", "mods-list.txt"),
		filepath.Join(root, ".varda", "neoforge-url.txt"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("unexpected persisted file %s: %v", path, err)
		}
	}
}

func TestCheckDoesNotRequireTargetDirOrCreateIt(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing-target")
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	raw := []byte(`{"schema_version":1,"pack":"varda","version":"0.1.4","minecraft":"1.21.1","neoforge":{"version":"21.1.228","installer_url":"https://maven.neoforged.net/releases/net/neoforged/neoforge/21.1.228/neoforge-21.1.228-installer.jar"},"mods":[{"filename":"example.jar","url":"https://example.invalid/example.jar"}]}`)
	if err := os.WriteFile(manifestPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Run([]string{"--dir", root, "--manifest-url", fileURL(manifestPath), "--check"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("check mode created target dir: %v", err)
	}
}

func TestExtractZipFileRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "traversal.zip")
	if err := writeTestZip(zipPath, map[string]string{"../evil.txt": "bad"}); err != nil {
		t.Fatal(err)
	}

	if err := extractZipFile(zipPath, root); err == nil || !strings.Contains(strings.ToLower(err.Error()), "escapes") {
		t.Fatalf("extractZipFile() error = %v, want traversal rejection", err)
	}
}

func TestExtractZipFileRejectsAbsolutePath(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "absolute.zip")
	if err := writeTestZip(zipPath, map[string]string{"/evil.txt": "bad"}); err != nil {
		t.Fatal(err)
	}

	if err := extractZipFile(zipPath, root); err == nil || !strings.Contains(strings.ToLower(err.Error()), "absolute") {
		t.Fatalf("extractZipFile() error = %v, want absolute path rejection", err)
	}
}

func TestReconcileModsRemovesUnmanagedFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	modsDirPath := filepath.Join(root, "mods")
	if err := os.MkdirAll(modsDirPath, 0o755); err != nil {
		t.Fatal(err)
	}

	source := filepath.Join(root, "source.jar")
	if err := os.WriteFile(source, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(modsDirPath, "extra.jar"), []byte("extra"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modsDirPath, "shader.zip"), []byte("zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modsDirPath, "notes.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modsDirPath, ".hidden"), []byte("hidden"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(modsDirPath, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := ReconcileMods(root, []ManifestMod{{Filename: "source.jar", URL: fileURL(source)}}, false, 6); err != nil {
		t.Fatalf("ReconcileMods() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(modsDirPath, "source.jar")); err != nil {
		t.Fatalf("source.jar missing after reconcile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(modsDirPath, "extra.jar")); !os.IsNotExist(err) {
		t.Fatalf("extra.jar still present after reconcile: %v", err)
	}
	for _, name := range []string{"shader.zip", "notes.txt", ".hidden"} {
		if _, err := os.Stat(filepath.Join(modsDirPath, name)); !os.IsNotExist(err) {
			t.Fatalf("%s still present after reconcile: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(modsDirPath, "nested")); !os.IsNotExist(err) {
		t.Fatalf("nested directory still present after reconcile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(modsDirPath, ".varda-desired-mods.txt")); !os.IsNotExist(err) {
		t.Fatalf(".varda-desired-mods.txt should not be created: %v", err)
	}
}

func TestInstallOrUpdateNeoForgeUsesDesiredManifest(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("fake installer"))
	}))
	defer server.Close()

	desired := NeoForgeManifest{
		Version:      "21.1.228",
		InstallerURL: server.URL + "/releases/net/neoforged/neoforge/21.1.228/neoforge-21.1.228-installer.jar",
	}

	oldJavaCommand := javaCommand
	javaCommand = func(name string, args ...string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=TestHelperJavaInstallerProcess", "--", filepath.Join(root, "libraries", "net", "neoforged", "neoforge", desired.Version))
		cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
		return cmd
	}
	defer func() { javaCommand = oldJavaCommand }()

	got, err := InstallOrUpdateNeoForge(root, desired, false)
	if err != nil {
		t.Fatalf("InstallOrUpdateNeoForge() error = %v", err)
	}
	if got != desired.Version {
		t.Fatalf("InstallOrUpdateNeoForge() = %q, want %q", got, desired.Version)
	}
}

func TestPatchLaunchersIncludesNogui(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	version := "21.1.83"
	if err := PatchLaunchers(root, version); err != nil {
		t.Fatalf("PatchLaunchers() error = %v", err)
	}

	runSh, err := os.ReadFile(filepath.Join(root, "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	runBat, err := os.ReadFile(filepath.Join(root, "run.bat"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(runSh), "nogui") || !strings.Contains(string(runBat), "nogui") {
		t.Fatalf("launchers missing nogui:\nrun.sh=%s\nrun.bat=%s", runSh, runBat)
	}
	if _, err := os.Stat(filepath.Join(root, "run.ps1")); !os.IsNotExist(err) {
		t.Fatalf("run.ps1 should not be generated: %v", err)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(root, "run.sh"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&0o111 == 0 {
			t.Fatalf("run.sh not executable")
		}
	}
}

func TestPatchRunBatAcceptsRealWorldForwardSlashes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	version := "21.1.228"
	content := strings.Join([]string{
		"@echo off",
		"REM Forge requires a configured set of both JVM and program arguments.",
		"REM Add custom JVM arguments to the user_jvm_args.txt",
		"REM Add custom program arguments {such as nogui} to this file in the next line before the %* or",
		"REM  pass them to this script directly",
		"java @user_jvm_args.txt @libraries/net/neoforged/neoforge/21.1.228/win_args.txt %*",
		"pause",
		"",
	}, "\r\n")
	if err := os.WriteFile(filepath.Join(root, "run.bat"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := patchRunBat(root, version); err != nil {
		t.Fatalf("patchRunBat() error = %v", err)
	}

	after, err := os.ReadFile(filepath.Join(root, "run.bat"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(after), "pause") {
		t.Fatalf("patched run.bat lost pause:\n%s", after)
	}
	if countLaunchNogui(strings.Split(string(after), "\r\n")) != 1 {
		t.Fatalf("expected exactly one nogui launch line:\n%s", after)
	}
	if !strings.Contains(string(after), "@libraries/net/neoforged/neoforge/21.1.228/win_args.txt nogui %*") {
		t.Fatalf("patched run.bat missing forward-slash launch line:\n%s", after)
	}

	if err := patchRunBat(root, version); err != nil {
		t.Fatalf("second patchRunBat() error = %v", err)
	}
	again, err := os.ReadFile(filepath.Join(root, "run.bat"))
	if err != nil {
		t.Fatal(err)
	}
	if countLaunchNogui(strings.Split(string(again), "\r\n")) != 1 {
		t.Fatalf("idempotency failed, nogui duplicated:\n%s", again)
	}
}

func TestPatchRunBatAcceptsBackslashes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	version := "21.1.228"
	content := "@echo off\r\njava @user_jvm_args.txt @libraries\\net\\neoforged\\neoforge\\21.1.228\\win_args.txt %*\r\npause\r\n"
	if err := os.WriteFile(filepath.Join(root, "run.bat"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := patchRunBat(root, version); err != nil {
		t.Fatalf("patchRunBat() error = %v", err)
	}

	after, err := os.ReadFile(filepath.Join(root, "run.bat"))
	if err != nil {
		t.Fatal(err)
	}
	if countLaunchNogui(strings.Split(string(after), "\r\n")) != 1 {
		t.Fatalf("expected exactly one nogui launch line:\n%s", after)
	}
}

func TestCleanupNeoForgeInstallerArtifacts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	files := []string{
		"neoforge-21.1.228-installer.jar",
		"neoforge-21.1.228-installer.jar.log",
		"installer.log",
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := cleanupNeoForgeInstallerArtifacts(root); err != nil {
		t.Fatalf("cleanupNeoForgeInstallerArtifacts() error = %v", err)
	}

	for _, name := range files {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("%s still present after cleanup: %v", name, err)
		}
	}
}

func TestReplaceFileRestoresExistingTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "artifact.jar")
	temp := filepath.Join(root, "artifact.jar.tmp")

	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(temp, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := replaceFile(temp, target); err != nil {
		t.Fatalf("replaceFile() error = %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("replaceFile() wrote %q, want %q", data, "new")
	}
	if _, err := os.Stat(target + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("backup file still present: %v", err)
	}
}

func TestReplaceFileCreatesMissingTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "artifact.jar")
	temp := filepath.Join(root, "artifact.jar.tmp")

	if err := os.WriteFile(temp, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := replaceFile(temp, target); err != nil {
		t.Fatalf("replaceFile() error = %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("replaceFile() wrote %q, want %q", data, "new")
	}
}

func TestReplaceFilePropagatesStatErrors(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "artifact.jar")
	temp := filepath.Join(root, "artifact.jar.tmp")

	if err := os.WriteFile(temp, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldStat := statFile
	statFile = func(string) (os.FileInfo, error) {
		return nil, os.ErrPermission
	}
	defer func() { statFile = oldStat }()

	if err := replaceFile(temp, target); !os.IsPermission(err) {
		t.Fatalf("replaceFile() error = %v, want permission error", err)
	}

	if _, err := os.Stat(temp); err != nil {
		t.Fatalf("temp file should remain after stat failure cleanup path: %v", err)
	}
}

func TestParseOptionsDefaultDownloadWorkers(t *testing.T) {
	t.Parallel()

	opts, err := parseOptions(nil)
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}
	if opts.DownloadWorkers != 6 {
		t.Fatalf("parseOptions() DownloadWorkers = %d, want 6", opts.DownloadWorkers)
	}
}

func TestParseOptionsRejectsInvalidDownloadWorkers(t *testing.T) {
	t.Parallel()

	if _, err := parseOptions([]string{"--download-workers", "0"}); err == nil {
		t.Fatalf("parseOptions() accepted 0 workers")
	}
	if _, err := parseOptions([]string{"--download-workers", "17"}); err == nil {
		t.Fatalf("parseOptions() accepted 17 workers")
	}
}

func validTestManifest() Manifest {
	return Manifest{
		SchemaVersion: 1,
		Pack:          "varda",
		Version:       "0.1.4",
		Minecraft:     "1.21.1",
		NeoForge: NeoForgeManifest{
			Version:      "21.1.228",
			InstallerURL: "https://maven.neoforged.net/releases/net/neoforged/neoforge/21.1.228/neoforge-21.1.228-installer.jar",
		},
		Mods: []ManifestMod{
			{
				Filename: "example.jar",
				URL:      "https://example.invalid/example.jar",
			},
		},
	}
}

func writeTestZip(path string, entries map[string]string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	zw := zip.NewWriter(file)
	for name, content := range entries {
		writer, err := zw.Create(name)
		if err != nil {
			zw.Close()
			return err
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			zw.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return nil
}

func fileURL(path string) string {
	slashes := filepath.ToSlash(path)
	if strings.HasPrefix(slashes, "/") {
		return "file://" + slashes
	}
	return "file:///" + slashes
}

func countLaunchNogui(lines []string) int {
	count := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(line), "java ") {
			continue
		}
		if strings.Count(strings.ToLower(line), " nogui ") == 1 {
			count++
		}
	}
	return count
}

func TestHelperJavaInstallerProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	args := os.Args
	for i, arg := range args {
		if arg != "--" || i+1 >= len(args) {
			continue
		}
		if err := os.MkdirAll(args[i+1], 0o755); err != nil {
			_, _ = os.Stderr.WriteString(err.Error())
			os.Exit(1)
		}
		os.Exit(0)
	}

	_, _ = os.Stderr.WriteString("missing helper target dir")
	os.Exit(1)
}
