# mds

Serve markdown files as HTML from the command line.

```sh
mds README.md              # render one file at http://127.0.0.1:8080/
mds ./docs                 # serve a directory with listings
mds -t md,png ./docs       # serve only .md and .png files
mds -x node_modules ./app  # skip node_modules
mds -p 3000 ./docs         # custom port
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
mds serve [flags] <path>   Same thing; use when the path is named like a
                           command, e.g. a directory called "config"

  -p, --port <n>       Port to listen on (default 8080)
      --host <addr>    Address to bind (default 0.0.0.0)
      --open           Open the page in a browser after starting
      --no-reload      Disable live reload
      --no-toc         Hide the table of contents
  -t, --types <list>   Only serve these file types, e.g. "md,txt,png"
  -x, --exclude <list> Glob patterns for names to skip (default ".git")
      --hidden         Also serve dot-prefixed files and directories
      --index          Render README.md / index.md under directory listings
      --no-index       Disable --index
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
4. environment: `MDS_HOST`, `MDS_PORT`, `MDS_TYPES`, `MDS_EXCLUDE`,
   `MDS_HIDDEN`, `MDS_RELOAD`, `MDS_INDEX`, `MDS_OPEN`, `MDS_TOC`
5. command-line flags

All keys are optional:

```toml
host = "127.0.0.1"                 # bind address
port = 3000
types = ["md", "png"]              # only serve these file types
exclude = [".git", "node_modules"] # glob patterns for names to skip
hidden = false                     # serve dot-prefixed files too
reload = true                      # live reload
index = true                       # render README.md / index.md under listings
open = false                       # open a browser after starting
toc = true                         # table of contents on rendered pages
```

`mds config [dir]` prints the effective values and the source of each. When
serving, the startup output also lists which sources are active, e.g.
`config: ~/.config/mds/config.toml, .mds.toml, MDS_HOST, --port`.

Subcommands take precedence over paths: `mds config` runs the command even if
a `config` directory exists. mds prints a note in that case; use `mds ./config`
or `mds serve config` to serve it.

- By default mds binds to all interfaces so other devices on your network
  (a phone, a Tailscale peer) can open it. Every reachable URL is printed at
  startup. On untrusted networks bind locally with `--host 127.0.0.1`.
- To stay local-only by default, put `host = "127.0.0.1"` in the global config
  or `export MDS_HOST=127.0.0.1` in your shell rc.
- With `index = true` (or `--index`), a directory that contains `README.md` or
  `index.md` shows it rendered below the listing, like GitHub.
- In directory mode, markdown files are rendered and everything else is served
  as-is. Dot-prefixed files are skipped unless `--hidden` is given; names
  matching an `exclude` pattern are never listed or served. Patterns use shell
  glob syntax and match a single file or directory name.
- `--open` opens `http://127.0.0.1:<port>/` in the local browser. Over SSH or
  without a display it prints a note and keeps serving.
- Documents with three or more headings get a table of contents: a sidebar on
  wide screens, a "Contents" button in the top bar on narrow ones. Type to
  filter headings, Enter jumps to the first match; `t` or `/` focuses the
  filter. A back-to-top button appears after scrolling down.
- YAML front matter (`---` block at the top) is hidden; its `title` field, if
  present, becomes the page title.
- ```mermaid blocks are rendered as diagrams (needs internet access in the
  browser: mermaid.js is loaded from a CDN on pages that contain a diagram).
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
