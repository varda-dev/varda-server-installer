# Varda Server Installer

Standalone installer and updater for the Varda modded Minecraft server.

It fetches desired server state from:

```text
https://varda-dev.github.io/varda-modpack/manifest.json
```

## Environment Setup
Create a .env file in the repo's root with  
```text
GITHUB_PAT_RELEASES="xxx"
```

## Requirements

- Java 21+
- Network access
- Linux, macOS, or Windows binary for target platform

## Install

```bash
tar -xzf varda-server-installer-0.1.4-linux-amd64.tar.gz
sudo install -m 0755 varda-server-installer /usr/local/bin/varda-server-installer
```

Release archives contain only the platform binary.

## Usage

```bash
varda-server-installer --dir /opt/minecraft/varda
```

Flags:

- `--manifest-url`
- `--check`
- `--force`
- `--download-workers`
- `--version`

Installer fetches a remote manifest.

## OpenRC

```sh
#!/sbin/openrc-run

name="Varda Minecraft Server"
description="Varda modded Minecraft server"

command="/opt/minecraft/varda/run.sh"
command_user="minecraft:minecraft"
directory="/opt/minecraft/varda"
pidfile="/run/varda-minecraft.pid"
command_background="yes"

depend() {
  need net
  after firewall
}

start_pre() {
  ebegin "Updating Varda Minecraft server"
  /usr/local/bin/varda-server-installer --dir /opt/minecraft/varda
  eend $?
}
```

## Release Flow

```bash
go test ./...
go tool build-release -v 0.1.4 -c
python gh-upload.py -v 0.1.4 -c "Varda server installer 0.1.4" --dry-run
python gh-upload.py -v 0.1.4 -c "Varda server installer 0.1.4"
```

Fallback, if `go tool` unavailable:

```bash
go run ./tools/build-release -v 0.1.4 -c
```

Build tool:

- builds platform binaries with size-reducing Go flags,
- packages release archives,
- writes `checksums.txt`.

Uploader:

- uploads release archives,
- uploads `checksums.txt`,

Final artifact names:

```text
varda-server-installer-0.1.4-windows-amd64.zip
varda-server-installer-0.1.4-linux-amd64.tar.gz
varda-server-installer-0.1.4-linux-arm64.tar.gz
varda-server-installer-0.1.4-darwin-amd64.tar.gz
varda-server-installer-0.1.4-darwin-arm64.tar.gz
checksums.txt
```

## varda-modpack

`varda-modpack` publishes `manifest.json` and server config artifacts. This repo only publishes installer archives and `checksums.txt`.
