# AGENTS.md

This repo is only standalone Varda server installer/updater.

Do not add:

- CurseForge upload logic
- modpack prep logic
- GitHub Pages publishing logic
- embedded modpack files or payload directories
- raw release binary publishing

Desired state comes from remote Varda modpack manifest:

```text
https://varda-dev.github.io/varda-modpack/manifest.json
```

Keep CLI script-friendly. Preserve:

- `--dir`
- `--manifest-url`
- `--check`
- `--force`
- `--download-workers`
- `--version`

No offline mode. No skip modes. Normal run always fetches remote manifest and converges server to it.
Do not persist manifest-derived desired state files like `.varda/manifest.json`, `.varda/mods-list.txt`, or `.varda/neoforge-url.txt`.
`--check` must not modify files and must not require target dir to exist.

Go:

- use `gofmt`
- prefer standard library
- keep behavior deterministic
- maintain tests

Python:

- root `gh-upload.py` only
- use `GITHUB_PAT_RELEASES`
- do not reintroduce `scripts/lib`
- do not reintroduce release types
- uploader only handles archives and `checksums.txt`

Release:

- use Go build tool for build, archive, checksums
- keep build and upload separate
- build tool owns cross-platform compile flags
- build tool owns archive packaging
- build tool owns `checksums.txt`
- `gh-upload.py` owns GitHub Release create/update and asset upload only
- do not upload raw binaries
- release archives should contain only the platform binary; do not bundle `README.md`, `LICENSE`, docs, manifests, or other extra files

Security:

- reject unsafe mod filenames
- reject unsafe manifest URLs where appropriate
- reject ZIP traversal and absolute ZIP paths
- verify manifest-provided artifact hashes; current manifest artifact hashes are SHA1
- do not execute manifest-provided commands
- only external executable path allowed is established Java call for NeoForge installer

Service focus:

- Linux/OpenRC first
- do not prioritize Windows service support
- avoid systemd-centric docs unless requested
