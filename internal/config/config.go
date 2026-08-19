package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const AppName = "session-recall"

// Config holds optional user overrides read from an rc file.
// An empty Config means "use built-in defaults".
type Config struct {
	// Database overrides the default index location
	// ($XDG_CACHE_HOME/session-recall/index.db).
	Database string `toml:"database"`
}

// Path returns the rc file location: $SESSION_RECALL_RC if set,
// otherwise the XDG config path (~/.config/session-recall/rc).
func Path(home, env string) string {
	if env != "" {
		return env
	}
	return filepath.Join(home, ".config", AppName, "rc")
}

// Load reads the rc file at path. A missing file is not an error and
// yields an empty Config.
func Load(path string) (Config, error) {
	var cfg Config
	_, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

// ExpandHome resolves a leading ~/ in path against home.
func ExpandHome(path, home string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}
