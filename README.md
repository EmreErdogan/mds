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
      --index          Render README.md / index.md under directory listings
      --no-index       Disable --index
      --no-reload      Disable live reload
  -h, --help           Show help

mds config [dir]       Show effective settings and where each comes from
mds update [--force]   Update to the latest release
mds version            Print the version
```

## Configuration

Settings are resolved in this order, later wins:

1. built-in defaults
2. global config: `~/.config/mds/config.toml` (or `$XDG_CONFIG_HOME/mds/config.toml`)
3. local config: `.mds.toml` in the served directory
4. environment: `MDS_HOST`, `MDS_PORT`, `MDS_EXT`, `MDS_RELOAD`, `MDS_INDEX`
5. command-line flags

All keys are optional:

```toml
host = "127.0.0.1"     # bind address
port = 3000
ext = ["md", "png"]    # only serve these extensions in directory mode
reload = true          # live reload
index = true           # render README.md / index.md under listings
```

`mds config [dir]` prints the effective values and the source of each.

- By default mds binds to all interfaces so other devices on your network
  (a phone, a Tailscale peer) can open it. Every reachable URL is printed at
  startup. On untrusted networks bind locally with `--host 127.0.0.1`.
- To stay local-only by default, put `host = "127.0.0.1"` in the global config
  or `export MDS_HOST=127.0.0.1` in your shell rc.
- With `index = true` (or `--index`), a directory that contains `README.md` or
  `index.md` shows it rendered below the listing, like GitHub.
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
