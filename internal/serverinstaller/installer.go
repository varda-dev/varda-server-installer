// Package serverinstaller provides the Varda Minecraft server installer and updater.
package serverinstaller

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

type Options struct {
	Force           bool
	DownloadWorkers int
	TargetDir       string
	ManifestURL     string
	Check           bool
	VersionOnly     bool
}

func Run(args []string) error {
	opts, err := parseOptions(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if opts.VersionOnly {
		fmt.Println(Version)
		return nil
	}

	if opts.TargetDir == "" {
		opts.TargetDir = "."
	}

	manifest, err := FetchRemoteManifest(opts.ManifestURL)
	if err != nil {
		return err
	}
	manifest.normalize()
	if err := manifest.Validate(); err != nil {
		return err
	}

	if opts.Check {
		fmt.Print(manifestCheckSummary(manifest))
		fmt.Println("Check complete. No files changed.")
		return nil
	}

	if err := os.MkdirAll(opts.TargetDir, 0o755); err != nil {
		return fmt.Errorf("create target dir: %w", err)
	}

	if err := installServerConfig(opts.TargetDir, manifest); err != nil {
		return err
	}

	if err := RequireJava21(); err != nil {
		return err
	}
	desiredNeoForgeVersion, err := InstallOrUpdateNeoForge(opts.TargetDir, manifest.NeoForge, opts.Force)
	if err != nil {
		return err
	}

	if err := WriteJvmArgs(opts.TargetDir, opts.Force); err != nil {
		return err
	}
	if err := PatchLaunchers(opts.TargetDir, desiredNeoForgeVersion); err != nil {
		return err
	}
	if err := cleanupNeoForgeInstallerArtifacts(opts.TargetDir); err != nil {
		return err
	}

	if err := ReconcileMods(opts.TargetDir, manifest.Mods, opts.Force, opts.DownloadWorkers); err != nil {
		return err
	}

	if err := WriteInstallDiagnostics(opts.TargetDir, manifest); err != nil {
		return err
	}

	fmt.Println("Setup complete.")
	return nil
}

func parseOptions(args []string) (Options, error) {
	var opts Options

	fs := flag.NewFlagSet("varda-server-installer", flag.ContinueOnError)
	fs.BoolVar(&opts.Force, "force", false, "re-download/reinstall files")
	fs.IntVar(&opts.DownloadWorkers, "download-workers", 6, "mod download worker count")
	fs.StringVar(&opts.TargetDir, "dir", ".", "server install directory")
	fs.StringVar(&opts.ManifestURL, "manifest-url", defaultManifestURL, "remote manifest URL")
	fs.BoolVar(&opts.Check, "check", false, "validate manifest and report actions without modifying files")
	fs.BoolVar(&opts.VersionOnly, "version", false, "print installer version and exit")

	if err := fs.Parse(args); err != nil {
		return opts, err
	}

	if opts.DownloadWorkers < 1 || opts.DownloadWorkers > 16 {
		return opts, fmt.Errorf("--download-workers must be between 1 and 16")
	}

	return opts, nil
}
