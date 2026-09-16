package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrecedence(t *testing.T) {
	global := t.TempDir()
	local := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", global)
	t.Setenv("HOME", t.TempDir())
	for _, v := range []string{"MDS_HOST", "MDS_PORT", "MDS_TYPES", "MDS_EXCLUDE", "MDS_HIDDEN", "MDS_RELOAD", "MDS_INDEX", "MDS_OPEN", "MDS_TOC"} {
		t.Setenv(v, "")
		os.Unsetenv(v)
	}

	// Nothing set: defaults.
	cfg, src, err := Load(local)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "0.0.0.0" || cfg.Port != 8080 || !cfg.Reload || cfg.Index || cfg.Hidden || cfg.Open || !cfg.TOC || src["host"] != "default" {
		t.Errorf("defaults wrong: %+v %v", cfg, src)
	}
	if len(cfg.Exclude) != 1 || cfg.Exclude[0] != ".git" {
		t.Errorf("defaults wrong: %+v %v", cfg, src)
	}

	// Global sets host and port; local overrides port and sets ext.
	os.MkdirAll(filepath.Join(global, "mds"), 0o755)
	os.WriteFile(filepath.Join(global, "mds", "config.toml"), []byte("host = \"127.0.0.1\"\nport = 9000\nindex = true\n"), 0o644)
	os.WriteFile(filepath.Join(local, ".mds.toml"), []byte("port = 9001\ntypes = [\"MD\", \".png\"]\nexclude = [\"node_modules\", \"*.log\"]\n"), 0o644)
	cfg, src, err = Load(local)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "127.0.0.1" || !cfg.Index {
		t.Errorf("global not applied: %+v", cfg)
	}
	if cfg.Port != 9001 || len(cfg.Types) != 2 || cfg.Types[0] != "md" || cfg.Types[1] != "png" {
		t.Errorf("local not applied: %+v", cfg)
	}
	if len(cfg.Exclude) != 2 || cfg.Exclude[1] != "*.log" {
		t.Errorf("exclude not applied: %+v", cfg.Exclude)
	}
	if src["port"] != "local ("+filepath.Join(local, ".mds.toml")+")" {
		t.Errorf("port source: %s", src["port"])
	}

	// Env beats both.
	t.Setenv("MDS_PORT", "9002")
	t.Setenv("MDS_RELOAD", "false")
	cfg, src, err = Load(local)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 9002 || cfg.Reload || src["port"] != "env MDS_PORT" {
		t.Errorf("env not applied: %+v %v", cfg, src)
	}

	// Bad values are errors.
	t.Setenv("MDS_PORT", "abc")
	if _, _, err := Load(local); err == nil {
		t.Error("expected error for bad MDS_PORT")
	}
	os.Unsetenv("MDS_PORT")
	os.WriteFile(filepath.Join(local, ".mds.toml"), []byte("prot = 1\n"), 0o644)
	if _, _, err := Load(local); err == nil {
		t.Error("expected error for unknown key")
	}
	os.WriteFile(filepath.Join(local, ".mds.toml"), []byte("ext = [\"md\"]\n"), 0o644)
	if _, _, err := Load(local); err == nil || !strings.Contains(err.Error(), "renamed") {
		t.Errorf("expected rename hint for ext, got %v", err)
	}
}
