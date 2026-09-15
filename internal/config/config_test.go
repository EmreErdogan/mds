package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrecedence(t *testing.T) {
	global := t.TempDir()
	local := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", global)
	t.Setenv("HOME", t.TempDir())
	for _, v := range []string{"MDS_HOST", "MDS_PORT", "MDS_EXT", "MDS_RELOAD", "MDS_INDEX"} {
		t.Setenv(v, "")
		os.Unsetenv(v)
	}

	// Nothing set: defaults.
	cfg, src, err := Load(local)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "0.0.0.0" || cfg.Port != 8080 || !cfg.Reload || cfg.Index || src["host"] != "default" {
		t.Errorf("defaults wrong: %+v %v", cfg, src)
	}

	// Global sets host and port; local overrides port and sets ext.
	os.MkdirAll(filepath.Join(global, "mds"), 0o755)
	os.WriteFile(filepath.Join(global, "mds", "config.toml"), []byte("host = \"127.0.0.1\"\nport = 9000\nindex = true\n"), 0o644)
	os.WriteFile(filepath.Join(local, ".mds.toml"), []byte("port = 9001\next = [\"MD\", \".png\"]\n"), 0o644)
	cfg, src, err = Load(local)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "127.0.0.1" || !cfg.Index {
		t.Errorf("global not applied: %+v", cfg)
	}
	if cfg.Port != 9001 || len(cfg.Ext) != 2 || cfg.Ext[0] != "md" || cfg.Ext[1] != "png" {
		t.Errorf("local not applied: %+v", cfg)
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
}
