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
	Host   string
	Port   int
	Ext    []string // allowed extensions in directory mode; empty = all
	Reload bool     // live reload
	Index  bool     // render README.md / index.md under directory listings
}

// Keys lists setting names in display order.
var Keys = []string{"host", "port", "ext", "reload", "index"}

// Sources records where each setting's effective value came from.
type Sources map[string]string

// Defaults returns the built-in configuration.
func Defaults() Config {
	return Config{Host: "0.0.0.0", Port: 8080, Reload: true, Index: false}
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
	Host   *string   `toml:"host"`
	Port   *int      `toml:"port"`
	Ext    *[]string `toml:"ext"`
	Reload *bool     `toml:"reload"`
	Index  *bool     `toml:"index"`
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
		return fmt.Errorf("%s: unknown setting %q", path, undecoded[0].String())
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
	if f.Ext != nil {
		cfg.Ext, src["ext"] = NormalizeExts(*f.Ext), where
	}
	if f.Reload != nil {
		cfg.Reload, src["reload"] = *f.Reload, where
	}
	if f.Index != nil {
		cfg.Index, src["index"] = *f.Index, where
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
	if v, ok := os.LookupEnv("MDS_EXT"); ok {
		cfg.Ext, src["ext"] = SplitExts(v), "env MDS_EXT"
	}
	for _, e := range []struct {
		name string
		dst  *bool
		key  string
	}{{"MDS_RELOAD", &cfg.Reload, "reload"}, {"MDS_INDEX", &cfg.Index, "index"}} {
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

// Format renders a setting value for display.
func Format(cfg Config, key string) string {
	switch key {
	case "host":
		return cfg.Host
	case "port":
		return strconv.Itoa(cfg.Port)
	case "ext":
		if len(cfg.Ext) == 0 {
			return "(all)"
		}
		return strings.Join(cfg.Ext, ",")
	case "reload":
		return strconv.FormatBool(cfg.Reload)
	case "index":
		return strconv.FormatBool(cfg.Index)
	}
	return ""
}
