# Changelog

All notable changes to this project are documented here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

## [0.14.0] - 2026-09-16

### Added
- Theme switcher in the top bar, cycling auto → light → dark. The choice is
  stored in the browser and overrides the server's `theme` setting for that
  reader. Mermaid diagrams re-render on change.

## [0.13.0] - 2026-09-16

### Added
- `theme` setting, `--theme` flag and `MDS_THEME`: `auto` follows the system
  (default), `light` and `dark` force a palette. Applies to the page, code
  highlighting and Mermaid diagrams.

### Fixed
- Code blocks in dark mode now use the page's code background instead of
  blending into the page.

## [0.12.0] - 2026-09-16

### Added
- `mds config init` writes a commented config file with every setting at its
  default to `~/.config/mds/config.toml`. `--local` writes `./.mds.toml`,
  `--force` overwrites an existing file.

## [0.11.0] - 2026-09-16

### Added
- Table of contents: a sticky "Contents" button in the top bar opens the
  heading list on narrow screens; the sidebar stays on wide screens.
- Heading filter in the table of contents. Enter jumps to the first match;
  `t` or `/` focuses the filter, Escape clears or closes it.
- Back-to-top button after scrolling more than one screen.
- Heading anchor links (`#` on hover), smooth scrolling, and headings no
  longer hide behind the sticky top bar when following an anchor.
- The active section stays visible in a long sidebar.

### Changed
- The top bar is now sticky.

## [0.10.0] - 2026-09-16

### Added
- Table of contents built from h2–h4 headings, shown as a sticky sidebar on
  wide screens and a collapsible block at the top on narrow ones. Appears on
  documents with at least three headings; the current section is highlighted.
  Disable with `--no-toc`, `toc = false` or `MDS_TOC=false`.

## [0.9.0] - 2026-09-16

### Added
- Mermaid diagrams: fenced ```mermaid blocks are rendered in the browser,
  following the light/dark theme. The renderer is loaded from jsDelivr only on
  pages that contain a diagram; offline, the block stays as code.

## [0.8.0] - 2026-09-16

### Added
- YAML front matter is parsed and hidden from the rendered page. A `title`
  field becomes the page title, taking precedence over the first heading.

## [0.7.0] - 2026-09-16

### Changed
- `--ext` / `-e` is now `--types` / `-t`; the config key `ext` is `types` and
  `MDS_EXT` is `MDS_TYPES`. A config file still using `ext` gets a rename hint.

### Added
- `--exclude` / `-x`, config `exclude`, `MDS_EXCLUDE`: glob patterns for file
  and directory names to hide and refuse to serve. Default `.git`.
- `--hidden`, config `hidden`, `MDS_HIDDEN`: serve dot-prefixed files.
- `--open`, config `open`, `MDS_OPEN`: open the local URL in a browser after
  starting. Without a display it prints a note and continues.

## [0.6.0] - 2026-09-15

### Added
- `mds serve <path>` as the explicit form of `mds <path>`, for paths that
  share a name with a subcommand.
- A note is printed when a subcommand name also exists as a file or directory.
- Startup output lists the active configuration sources (config files,
  environment variables, flags) when any non-default source is in effect.

## [0.5.0] - 2026-09-15

### Added
- Configuration files: global `~/.config/mds/config.toml` and per-directory
  `.mds.toml`. Precedence: defaults < global < local < environment < flags.
- `mds config [dir]` shows effective settings and where each one comes from.
- `index` setting / `--index` flag: render `README.md` or `index.md` below
  directory listings.
- `MDS_EXT`, `MDS_RELOAD` and `MDS_INDEX` environment variables.

## [0.4.0] - 2026-09-15

### Added
- Syntax highlighting for fenced code blocks (chroma), with light and dark
  palettes following the system theme.

## [0.3.0] - 2026-09-15

### Added
- Live reload: pages reload automatically when the markdown file or the
  listed directory changes. Only watched paths are subscribed, so serving a
  large tree stays cheap. Disable with `--no-reload`.

## [0.2.0] - 2026-09-15

### Changed
- Default bind address is now `0.0.0.0` so other devices on the network can
  reach the server. Use `--host 127.0.0.1` for local-only.
- Startup output lists every reachable URL with its interface name.

### Added
- `MDS_HOST` and `MDS_PORT` environment variables; flags take precedence.

## [0.1.0] - 2026-09-15

### Added
- Render a single markdown file as HTML: `mds README.md`.
- Serve a directory with listings and rendered markdown: `mds ./docs`.
- `--ext` / `-e` to restrict served file types in directory mode.
- `--port` / `-p` and `--host` flags.
- GitHub Flavored Markdown (tables, task lists, strikethrough, autolinks).
- `?raw` query to view the markdown source.
- `mds update` (alias `upgrade`) to self-update from GitHub releases.
- `mds version`.
- `install.sh` installer that adds the binary to PATH.
