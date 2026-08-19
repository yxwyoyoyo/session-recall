package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathUsesXDGDefault(t *testing.T) {
	if got := Path("/home/user", ""); got != "/home/user/.config/session-recall/rc" {
		t.Fatalf("Path = %q", got)
	}
}

func TestPathHonorsEnvOverride(t *testing.T) {
	if got := Path("/home/user", "/tmp/my-rc.conf"); got != "/tmp/my-rc.conf" {
		t.Fatalf("Path = %q", got)
	}
}

func TestLoadMissingFileReturnsEmpty(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "no-such-rc"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database != "" {
		t.Fatalf("unexpected database %q", cfg.Database)
	}
}

func TestLoadReadsDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rc")
	if err := os.WriteFile(path, []byte("database = \"~/data/index.db\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database != "~/data/index.db" {
		t.Fatalf("database = %q", cfg.Database)
	}
}

func TestLoadRejectsMalformedRc(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rc")
	if err := os.WriteFile(path, []byte("database = \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected parse error for malformed rc")
	}
}

func TestExpandHome(t *testing.T) {
	cases := []struct{ in, home, want string }{
		{"~/a/b", "/home/u", "/home/u/a/b"},
		{"~", "/home/u", "/home/u"},
		{"/abs/path", "/home/u", "/abs/path"},
		{"relative.db", "/home/u", "relative.db"},
	}
	for _, c := range cases {
		if got := ExpandHome(c.in, c.home); got != c.want {
			t.Fatalf("ExpandHome(%q, %q) = %q, want %q", c.in, c.home, got, c.want)
		}
	}
}
