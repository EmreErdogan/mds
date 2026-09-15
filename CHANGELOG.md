# Changelog

All notable changes to this project are documented here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

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
