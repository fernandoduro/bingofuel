package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/fernandoduro/bingofuel/internal/config"
	"github.com/fernandoduro/bingofuel/internal/engine"
	"github.com/fernandoduro/bingofuel/internal/model"
	"github.com/fernandoduro/bingofuel/internal/providers"
	"github.com/fernandoduro/bingofuel/internal/secrets"
)

func cmdDoctor(_ []string) int {
	problems := 0
	ok := func(label, v string) { fmt.Printf("  ✓ %-18s %s\n", label, v) }
	bad := func(label, v string) { problems++; fmt.Printf("  ✗ %-18s %s\n", label, v) }
	info := func(label, v string) { fmt.Printf("    %-18s %s\n", label, v) }

	fmt.Println("Bingo Fuel", Version)
	cfg, err := config.Load()
	switch {
	case err != nil:
		bad("config", err.Error())
	case cfg.Found:
		ok("config", cfg.Path)
	default:
		info("config", "none yet (defaults in use): run bingofuel setup")
	}
	cache, _ := config.CacheDir()
	state, _ := config.StateDir()
	info("cache", cache)
	info("state", state)
	if err := secrets.KeyringAvailable(); err != nil {
		info("keyring", "unavailable ("+err.Error()+"): keys use the 0600 fallback file")
	} else {
		ok("keyring", "available")
	}

	e, err := engine.New(cfg)
	if err != nil {
		bad("engine", err.Error())
		return 1
	}
	now := time.Now()
	// Live check: fetch everything not held back by a 429.
	e.MaxAge = 0
	before := map[string]*time.Time{}
	for _, p := range engine.Enabled(cfg) {
		before[p.ID()] = e.LastSuccess(p.ID())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	out := e.Run(ctx)

	for _, g := range out.Groups {
		fmt.Printf("\n%s\n", g.Title)
		switch g.ID {
		case "claude":
			if src, err := (providers.Claude{Cfg: cfg.Claude}).CredentialSource(); err != nil {
				bad("login", err.Error())
			} else {
				ok("login", src)
			}
		case "codex":
			if src, err := (providers.Codex{Cfg: cfg.Codex}).CredentialSource(); err != nil {
				bad("login", err.Error())
			} else {
				ok("login", src)
			}
		case "cursor":
			if src, err := (providers.Cursor{Cfg: cfg.Cursor}).CredentialSource(); err != nil {
				bad("login", err.Error())
			} else {
				ok("login", src)
			}
		case "openrouter":
			keyLine("API key", "openrouter", cfg.OpenRouter.KeyEnv, ok, bad)
		case "typesafe":
			keyLine("API key", "typesafe", cfg.TypeSafe.KeyEnv, ok, info)
			ts := providers.TypeSafe{Cfg: cfg.TypeSafe}
			if l, err := ts.LoadLedger(); err != nil {
				bad("ledger", err.Error())
			} else if len(l.Checkpoints) == 0 {
				bad("ledger", "no balance entered: bingofuel jev balance <USD>")
			} else {
				c := l.Checkpoints[len(l.Checkpoints)-1]
				ok("ledger", fmt.Sprintf("$%.2f entered %s", c.BalanceUSD, c.At.Local().Format("2006-01-02 15:04")))
			}
			if len(cfg.TypeSafe.UsageLogs) == 0 {
				info("usage logs", "none configured: the estimate never goes down")
			}
			for _, p := range cfg.TypeSafe.UsageLogs {
				if _, err := os.Stat(config.Expand(p)); err != nil {
					bad("usage log", p+" (unreadable)")
				} else {
					ok("usage log", p)
				}
			}
		}
		if g.Status == model.StatusOK || g.Status == model.StatusRateLimited {
			ok("data", summarize(g))
		} else {
			bad("data", fmt.Sprintf("%s: %s", g.Status, g.Message))
		}
		if !providerLocal(g.ID) {
			if ls := before[g.ID]; ls != nil {
				info("previous success", Ago(now.Sub(*ls))+" ago")
			} else {
				info("previous success", "never")
			}
		}
	}
	fmt.Println()
	if problems > 0 {
		fmt.Printf("%d problem(s) found.\n", problems)
		return 1
	}
	fmt.Println("All good.")
	return 0
}

func providerLocal(id string) bool { return id == "typesafe" }

func keyLine(label, provider, env string, ok func(string, string), bad func(string, string)) {
	if _, src, err := secrets.Get(provider, env); err != nil {
		bad(label, "not found: run bingofuel setup")
	} else {
		ok(label, "found in "+describeSource(src))
	}
}

func summarize(g model.Group) string {
	s := ""
	for i, m := range g.Meters {
		if i > 0 {
			s += ", "
		}
		if m.Kind == model.KindCredit && m.Remaining != nil {
			s += m.Label + " " + Money(*m.Remaining, m.Currency) + " left"
		} else {
			s += fmt.Sprintf("%s %.0f%%", m.Label, m.Percent)
		}
	}
	if s == "" {
		s = "no meters"
	}
	return s
}
