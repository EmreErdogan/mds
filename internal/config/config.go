// Package config resolves mds settings from, in increasing precedence:
// built-in defaults, the global config file, the local config file, and
// environment variables. Command-line flags are applied by the caller on top.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config holds every user-tunable setting.
type Config struct {
	Host    string
	Port    int
	Types   []string // allowed extensions in directory mode; empty = all
	Exclude []string // glob patterns for names to hide and refuse to serve
	Hidden  bool     // serve dot-prefixed files and directories
	Reload  bool     // live reload
	Index   bool     // render README.md / index.md under directory listings
	Open    bool     // open a browser after the server starts
	TOC     bool     // show a table of contents on rendered pages
	Theme   string   // "auto", "light" or "dark"
}

// Themes lists the accepted theme values.
var Themes = []string{"auto", "light", "dark"}

// Keys lists setting names in display order.
var Keys = []string{"host", "port", "types", "exclude", "hidden", "reload", "index", "open", "toc", "theme"}

// Sources records where each setting's effective value came from.
type Sources map[string]string

// Defaults returns the built-in configuration.
func Defaults() Config {
	return Config{Host: "0.0.0.0", Port: 8080, Exclude: []string{".git"}, Reload: true, TOC: true, Theme: "auto"}
}

// GlobalPath returns the global config file location:
// $XDG_CONFIG_HOME/mds/config.toml, defaulting to ~/.config/mds/config.toml.
func GlobalPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "mds", "config.toml")
}

// LocalName is the per-directory config file name.
const LocalName = ".mds.toml"

// LocalPath returns the local config file location for a served directory.
func LocalPath(dir string) string { return filepath.Join(dir, LocalName) }

// file mirrors Config with optional fields so absent keys can be detected.
type file struct {
	Host    *string   `toml:"host"`
	Port    *int      `toml:"port"`
	Types   *[]string `toml:"types"`
	Exclude *[]string `toml:"exclude"`
	Hidden  *bool     `toml:"hidden"`
	Reload  *bool     `toml:"reload"`
	Index   *bool     `toml:"index"`
	Open    *bool     `toml:"open"`
	TOC     *bool     `toml:"toc"`
	Theme   *string   `toml:"theme"`
}

// Load resolves the configuration for serving localDir.
func Load(localDir string) (Config, Sources, error) {
	cfg := Defaults()
	src := Sources{}
	for _, k := range Keys {
		src[k] = "default"
	}

	for _, layer := range []struct{ label, path string }{
		{"global", GlobalPath()},
		{"local", LocalPath(localDir)},
	} {
		if layer.path == "" {
			continue
		}
		if err := applyFile(&cfg, src, layer.label, layer.path); err != nil {
			return cfg, src, err
		}
	}
	if err := applyEnv(&cfg, src); err != nil {
		return cfg, src, err
	}
	return cfg, src, nil
}

func applyFile(cfg *Config, src Sources, label, path string) error {
	var f file
	md, err := toml.DecodeFile(path, &f)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("%s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		key := undecoded[0].String()
		if key == "ext" {
			return fmt.Errorf("%s: setting \"ext\" was renamed to \"types\"", path)
		}
		return fmt.Errorf("%s: unknown setting %q", path, key)
	}
	where := label + " (" + path + ")"
	if f.Host != nil {
		cfg.Host, src["host"] = *f.Host, where
	}
	if f.Port != nil {
		if err := checkPort(*f.Port); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		cfg.Port, src["port"] = *f.Port, where
	}
	if f.Types != nil {
		cfg.Types, src["types"] = NormalizeExts(*f.Types), where
	}
	if f.Exclude != nil {
		cfg.Exclude, src["exclude"] = NormalizeList(*f.Exclude), where
	}
	if f.Hidden != nil {
		cfg.Hidden, src["hidden"] = *f.Hidden, where
	}
	if f.Reload != nil {
		cfg.Reload, src["reload"] = *f.Reload, where
	}
	if f.Index != nil {
		cfg.Index, src["index"] = *f.Index, where
	}
	if f.Open != nil {
		cfg.Open, src["open"] = *f.Open, where
	}
	if f.TOC != nil {
		cfg.TOC, src["toc"] = *f.TOC, where
	}
	if f.Theme != nil {
		if err := CheckTheme(*f.Theme); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		cfg.Theme, src["theme"] = *f.Theme, where
	}
	return nil
}

func applyEnv(cfg *Config, src Sources) error {
	if v, ok := os.LookupEnv("MDS_HOST"); ok && v != "" {
		cfg.Host, src["host"] = v, "env MDS_HOST"
	}
	if v, ok := os.LookupEnv("MDS_PORT"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || checkPort(n) != nil {
			return fmt.Errorf("invalid MDS_PORT %q", v)
		}
		cfg.Port, src["port"] = n, "env MDS_PORT"
	}
	if v, ok := os.LookupEnv("MDS_TYPES"); ok {
		cfg.Types, src["types"] = SplitExts(v), "env MDS_TYPES"
	}
	if v, ok := os.LookupEnv("MDS_EXCLUDE"); ok {
		cfg.Exclude, src["exclude"] = SplitList(v), "env MDS_EXCLUDE"
	}
	if v, ok := os.LookupEnv("MDS_THEME"); ok && v != "" {
		if err := CheckTheme(v); err != nil {
			return fmt.Errorf("MDS_THEME: %w", err)
		}
		cfg.Theme, src["theme"] = v, "env MDS_THEME"
	}
	for _, e := range []struct {
		name string
		dst  *bool
		key  string
	}{
		{"MDS_HIDDEN", &cfg.Hidden, "hidden"},
		{"MDS_RELOAD", &cfg.Reload, "reload"},
		{"MDS_INDEX", &cfg.Index, "index"},
		{"MDS_OPEN", &cfg.Open, "open"},
		{"MDS_TOC", &cfg.TOC, "toc"},
	} {
		v, ok := os.LookupEnv(e.name)
		if !ok || v == "" {
			continue
		}
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("invalid %s %q (want true/false)", e.name, v)
		}
		*e.dst, src[e.key] = b, "env "+e.name
	}
	return nil
}

// CheckTheme validates a theme value.
func CheckTheme(v string) error {
	for _, t := range Themes {
		if v == t {
			return nil
		}
	}
	return fmt.Errorf("invalid theme %q (want auto, light or dark)", v)
}

func checkPort(n int) error {
	if n < 0 || n > 65535 {
		return fmt.Errorf("port %d out of range", n)
	}
	return nil
}

// SplitExts parses a comma-separated extension list.
func SplitExts(s string) []string {
	return NormalizeExts(strings.Split(s, ","))
}

// NormalizeExts lowercases extensions and strips dots and blanks.
func NormalizeExts(in []string) []string {
	out := []string{}
	for _, e := range in {
		e = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(e), "."))
		if e != "" {
			out = append(out, e)
		}
	}
	return out
}

// SplitList parses a comma-separated list, dropping blanks.
func SplitList(s string) []string {
	return NormalizeList(strings.Split(s, ","))
}

// NormalizeList trims entries and drops blanks.
func NormalizeList(in []string) []string {
	out := []string{}
	for _, e := range in {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	return out
}

// Format renders a setting value for display.
func Format(cfg Config, key string) string {
	switch key {
	case "host":
		return cfg.Host
	case "port":
		return strconv.Itoa(cfg.Port)
	case "types":
		if len(cfg.Types) == 0 {
			return "(all)"
		}
		return strings.Join(cfg.Types, ",")
	case "exclude":
		if len(cfg.Exclude) == 0 {
			return "(none)"
		}
		return strings.Join(cfg.Exclude, ",")
	case "hidden":
		return strconv.FormatBool(cfg.Hidden)
	case "reload":
		return strconv.FormatBool(cfg.Reload)
	case "index":
		return strconv.FormatBool(cfg.Index)
	case "open":
		return strconv.FormatBool(cfg.Open)
	case "toc":
		return strconv.FormatBool(cfg.TOC)
	case "theme":
		return cfg.Theme
	}
	return ""
}

// Template is a commented config file with every setting at its default.
const Template = `# mds configuration
# Every setting is optional. Uncomment a line to change it.
# Precedence: defaults < this file < <dir>/.mds.toml < environment < flags

# Address to bind. Use "127.0.0.1" to stay local-only.
#host = "0.0.0.0"

# Port to listen on.
#port = 8080

# Only serve these file types in directory mode (empty = all files).
#types = ["md", "png"]

# Glob patterns for file and directory names to hide and never serve.
#exclude = [".git", "node_modules"]

# Serve dot-prefixed files and directories too.
#hidden = false

# Reload pages in the browser when files change.
#reload = true

# Render README.md / index.md below directory listings.
#index = false

# Open the local URL in a browser after starting.
#open = false

# Show a table of contents on rendered pages.
#toc = true

# Color theme: "auto" follows the system, or force "light" / "dark".
#theme = "auto"
`

// Init writes Template to path. It refuses to overwrite an existing file
// unless force is set.
func Init(path string, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists (use --force to overwrite)", path)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(Template), 0o644)
}
