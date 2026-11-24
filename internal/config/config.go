// Package config loads ~/.config/bingofuel/config.toml (or the platform
// equivalent) and resolves the directories Bingo Fuel writes to.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"
)

const AppName = "bingofuel"

type Config struct {
	General    General    `toml:"general"`
	Claude     Claude     `toml:"claude"`
	Codex      Codex      `toml:"codex"`
	Cursor     Cursor     `toml:"cursor"`
	OpenRouter OpenRouter `toml:"openrouter"`
	TypeSafe   TypeSafe   `toml:"typesafe"`

	// Found is false when no file existed and Defaults() was returned.
	Found bool   `toml:"-"`
	Path  string `toml:"-"`
}

type General struct {
	// MaxAgeSeconds is the floor between two network fetches of one provider.
	MaxAgeSeconds int `toml:"max_age_seconds"`
	// Order lists provider ids in display order.
	Order []string `toml:"order"`
}

type Claude struct {
	Enabled bool `toml:"enabled"`
	// Dir is the Claude Code config folder. Empty: $CLAUDE_CONFIG_DIR, then ~/.claude.
	Dir string `toml:"dir"`
	// Meters shown as rings: session, weekly, model_weekly (top model), or any
	// meter id printed by `bingofuel fetch`. Empty: every reported meter.
	Meters []string `toml:"meters"`
}

type Codex struct {
	Enabled bool `toml:"enabled"`
	// Home is the Codex folder. Empty: $CODEX_HOME, then ~/.codex.
	Home   string   `toml:"home"`
	Meters []string `toml:"meters"` // empty: every reported window
}

type Cursor struct {
	Enabled bool `toml:"enabled"`
	// DB is Cursor's state.vscdb. Empty: <user config dir>/Cursor/User/globalStorage/state.vscdb.
	DB string `toml:"db"`
	// Meters shown as rings: auto, api, total. Empty: every reported meter.
	// total blends the two pools, so it is off by default.
	Meters []string `toml:"meters"`
}

type OpenRouter struct {
	Enabled bool `toml:"enabled"`
	// KeyEnv is an environment variable consulted after the keyring and file.
	KeyEnv string `toml:"key_env"`
}

type TypeSafe struct {
	Enabled      bool    `toml:"enabled"`
	KeyEnv       string  `toml:"key_env"`
	PricePerMTok float64 `toml:"price_per_mtok"`
	// UsageLogs are JSONL files whose JEV calls are subtracted from the balance.
	UsageLogs []string `toml:"usage_logs"`
}

func Defaults() Config {
	return Config{
		General:    General{MaxAgeSeconds: 300, Order: []string{"claude", "codex", "cursor", "openrouter", "typesafe"}},
		Claude:     Claude{Enabled: true, Meters: []string{"session", "weekly"}},
		Codex:      Codex{Enabled: true},
		Cursor:     Cursor{Enabled: true, Meters: []string{"auto", "api"}},
		OpenRouter: OpenRouter{Enabled: true, KeyEnv: "OPENROUTER_API_KEY"},
		TypeSafe:   TypeSafe{Enabled: true, KeyEnv: "TYPESAFE_API_KEY", PricePerMTok: 0.042},
	}
}

// Dir is where config.toml and the fallback secrets file live.
func Dir() (string, error) {
	if d := os.Getenv("BINGOFUEL_CONFIG_DIR"); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, AppName), nil
}

func CacheDir() (string, error) {
	if d := os.Getenv("BINGOFUEL_CACHE_DIR"); d != "" {
		return d, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, AppName), nil
}

// StateDir holds data the user entered that is not configuration (the JEV
// ledger). XDG_STATE_HOME on Linux, the config dir elsewhere.
func StateDir() (string, error) {
	if d := os.Getenv("BINGOFUEL_STATE_DIR"); d != "" {
		return d, nil
	}
	if runtime.GOOS == "linux" {
		if s := os.Getenv("XDG_STATE_HOME"); s != "" {
			return filepath.Join(s, AppName), nil
		}
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, ".local", "state", AppName), nil
		}
	}
	return Dir()
}

func Load() (Config, error) {
	dir, err := Dir()
	if err != nil {
		return Defaults(), err
	}
	return LoadFile(filepath.Join(dir, "config.toml"))
}

func LoadFile(path string) (Config, error) {
	cfg := Defaults()
	cfg.Path = path
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return Defaults(), fmt.Errorf("%s: %w", path, err)
	}
	cfg.Found = true
	if cfg.General.MaxAgeSeconds <= 0 {
		cfg.General.MaxAgeSeconds = 300
	}
	if len(cfg.General.Order) == 0 {
		cfg.General.Order = Defaults().General.Order
	}
	if cfg.TypeSafe.PricePerMTok <= 0 {
		cfg.TypeSafe.PricePerMTok = 0.042
	}
	return cfg, nil
}

func Save(cfg Config) error {
	path := cfg.Path
	if path == "" {
		dir, err := Dir()
		if err != nil {
			return err
		}
		path = filepath.Join(dir, "config.toml")
	}
	var b strings.Builder
	b.WriteString("# Bingo Fuel configuration. Re-run `bingofuel setup` to change it interactively.\n")
	b.WriteString("# Ring colours, thresholds and layout live in the widget's own settings.\n\n")
	if err := toml.NewEncoder(&b).Encode(cfg); err != nil {
		return err
	}
	return WriteFileAtomic(path, []byte(b.String()), 0o600)
}

// Expand resolves a leading ~ to the home directory.
func Expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// WriteFileAtomic writes via a temp file and rename, so a reader never sees
// a half-written file.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	if d, err := os.Open(filepath.Dir(path)); err == nil { // persist the rename
		_ = d.Sync()
		d.Close()
	}
	return nil
}
