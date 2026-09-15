# mds

Serve markdown files as HTML from the command line.

```sh
mds README.md        # render one file at http://127.0.0.1:8080/
mds ./docs           # serve a directory with listings
mds -e md,png ./docs # serve only .md and .png files
mds -p 3000 ./docs   # custom port
```

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/EmreErdogan/mds/main/install.sh | sh
```

The installer puts `mds` in `~/.local/bin` (override with `MDS_INSTALL_DIR`)
and adds that directory to your PATH. Pin a version with `MDS_VERSION=v0.1.0`.

From source:

```sh
go install github.com/EmreErdogan/mds@latest
```

## Update

```sh
mds update
```

## Usage

```
mds [flags] <file.md | directory>

  -p, --port <n>       Port to listen on (default 8080)
      --host <addr>    Address to bind (default 0.0.0.0)
  -e, --ext <list>     Comma-separated extensions to serve in directory mode
      --no-reload      Disable live reload
  -h, --help           Show help

mds update [--force]   Update to the latest release
mds version            Print the version
```

- By default mds binds to all interfaces so other devices on your network
  (a phone, a Tailscale peer) can open it. Every reachable URL is printed at
  startup. On untrusted networks bind locally with `--host 127.0.0.1`.
- Environment variables `MDS_HOST` and `MDS_PORT` set defaults; flags override
  them. Put `export MDS_HOST=127.0.0.1` in your shell rc to stay local-only.
- In directory mode, markdown files are rendered and everything else is served
  as-is. Hidden files (dot-prefixed) are never served.
- Fenced code blocks are syntax-highlighted; the palette follows the system
  light/dark preference.
- Pages reload automatically when the file you are viewing (or the directory
  listing) changes on disk. `--no-reload` turns this off.
- Append `?raw` to any markdown URL to see the source.
- In single-file mode the file is served at `/`; images and other assets next
  to it resolve normally.

## Development

```sh
make build   # bin/mds
make test
make dist    # cross-compiled binaries + checksums in dist/
```

Releases are created by pushing a `v*` tag; the workflow builds binaries and
publishes a GitHub release with notes taken from `CHANGELOG.md`.
