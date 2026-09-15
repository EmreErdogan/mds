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
      --host <addr>    Address to bind (default 127.0.0.1)
  -e, --ext <list>     Comma-separated extensions to serve in directory mode
  -h, --help           Show help

mds update [--force]   Update to the latest release
mds version            Print the version
```

- In directory mode, markdown files are rendered and everything else is served
  as-is. Hidden files (dot-prefixed) are never served.
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
