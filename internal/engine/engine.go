// Package engine decides, in one place, whether each provider is fetched or
// served from cache, and assembles the Output every front-end reads.
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fernandoduro/bingofuel/internal/config"
	"github.com/fernandoduro/bingofuel/internal/model"
	"github.com/fernandoduro/bingofuel/internal/providers"
)

const (
	// RateLimitHold is how long a 429 keeps the last figures as current.
	RateLimitHold = 30 * time.Minute
	// ErrorRetry is the floor between retries after a failed fetch.
	ErrorRetry = 60 * time.Second
)

// Entry is one provider's cache file. It holds only normalized data.
type Entry struct {
	Group       model.Group `json:"group"`
	FetchedAt   time.Time   `json:"fetched_at"`             // last network attempt
	LastSuccess *time.Time  `json:"last_success,omitempty"` // last usable answer
	HoldUntil   *time.Time  `json:"hold_until,omitempty"`   // 429 back-off
}

type Engine struct {
	Cfg      config.Config
	CacheDir string
	MaxAge   time.Duration
	Now      func() time.Time
	// Providers overrides the configured set (tests).
	Providers []providers.Provider
}

func New(cfg config.Config) (*Engine, error) {
	dir, err := config.CacheDir()
	if err != nil {
		return nil, err
	}
	return &Engine{Cfg: cfg, CacheDir: dir, MaxAge: time.Duration(cfg.General.MaxAgeSeconds) * time.Second, Now: time.Now}, nil
}

// Enabled lists the configured providers in display order.
func Enabled(cfg config.Config) []providers.Provider {
	all := map[string]providers.Provider{}
	if cfg.Claude.Enabled {
		all["claude"] = providers.Claude{Cfg: cfg.Claude}
	}
	if cfg.Codex.Enabled {
		all["codex"] = providers.Codex{Cfg: cfg.Codex}
	}
	if cfg.Cursor.Enabled {
		all["cursor"] = providers.Cursor{Cfg: cfg.Cursor}
	}
	if cfg.OpenRouter.Enabled {
		all["openrouter"] = providers.OpenRouter{Cfg: cfg.OpenRouter}
	}
	if cfg.TypeSafe.Enabled {
		all["typesafe"] = providers.TypeSafe{Cfg: cfg.TypeSafe}
	}
	var out []providers.Provider
	for _, id := range cfg.General.Order {
		if p, ok := all[id]; ok {
			out = append(out, p)
			delete(all, id)
		}
	}
	for _, id := range []string{"claude", "codex", "cursor", "openrouter", "typesafe"} {
		if p, ok := all[id]; ok {
			out = append(out, p)
		}
	}
	return out
}

func (e *Engine) providers() []providers.Provider {
	if e.Providers != nil {
		return e.Providers
	}
	return Enabled(e.Cfg)
}

// Run produces the output, fetching providers in parallel.
func (e *Engine) Run(ctx context.Context) model.Output {
	ps := e.providers()
	groups := make([]model.Group, len(ps))
	var wg sync.WaitGroup
	for i, p := range ps {
		wg.Add(1)
		go func(i int, p providers.Provider) {
			defer wg.Done()
			g := e.one(ctx, p)
			// Re-applied on every run so a config change shows without a refetch.
			providers.ApplySelection(g.Meters, p.Selection())
			if g.Meters == nil {
				g.Meters = []model.Meter{}
			}
			groups[i] = g
		}(i, p)
	}
	wg.Wait()
	return model.Output{Schema: model.SchemaVersion, FetchedAt: e.Now(), ConfigFound: e.Cfg.Found, Groups: groups}
}

func (e *Engine) one(ctx context.Context, p providers.Provider) model.Group {
	now := e.Now()
	if p.Local() {
		g, err := p.Fetch(ctx, now)
		return finish(g, err, now)
	}
	c, hasCache := e.load(p.ID())
	if hasCache && !e.due(c, now) {
		return c.Group
	}
	g, err := p.Fetch(ctx, now)
	if err == nil {
		g.Status = model.StatusOK
		g.LastSuccess = model.T(now)
		e.save(p.ID(), Entry{Group: g, FetchedAt: now, LastSuccess: g.LastSuccess})
		return g
	}

	status, msg := providers.StatusOf(err)
	next := Entry{FetchedAt: now}
	if hasCache {
		next.LastSuccess = c.LastSuccess
		next.Group = c.Group // keep the last figures, re-labelled below
	} else {
		next.Group = g
	}
	switch status {
	case model.StatusRateLimited:
		next.HoldUntil = model.T(now.Add(RateLimitHold))
		hold := next.HoldUntil.Local().Format("15:04")
		switch {
		case !hasCache || len(c.Group.Meters) == 0:
			// Nothing to hold: show it as missing data, not as current.
			status, msg = model.StatusStale, fmt.Sprintf("%s; no data yet, retrying after %s", msg, hold)
		case c.Group.Status != model.StatusOK && c.Group.Status != model.StatusRateLimited:
			// The cached figures were already stale; a 429 doesn't make them current.
			status, msg = c.Group.Status, fmt.Sprintf("%s; %s; retrying after %s", c.Group.Message, msg, hold)
		default:
			msg = fmt.Sprintf("%s; holding the last figures until %s", msg, hold)
		}
	case model.StatusPlanLapsed, model.StatusNotConfigured:
		next.Group.Meters = nil // never carry figures from a plan that is gone
	}
	next.Group.Status = status
	next.Group.Message = msg
	next.Group.LastSuccess = next.LastSuccess
	e.save(p.ID(), next)
	return next.Group
}

// due reports whether a provider should hit the network now.
func (e *Engine) due(c Entry, now time.Time) bool {
	if c.HoldUntil != nil && now.Before(*c.HoldUntil) {
		return false
	}
	age := now.Sub(c.FetchedAt)
	if age < 0 { // clock moved backwards
		return true
	}
	if c.Group.Status != model.StatusOK && c.Group.Status != model.StatusRateLimited {
		return age >= min(ErrorRetry, e.MaxAge)
	}
	// A window that reset since the last fetch makes the cached figure wrong.
	for _, m := range c.Group.Meters {
		if m.ResetsAt != nil && m.ResetsAt.After(c.FetchedAt) && !m.ResetsAt.After(now) {
			return true
		}
	}
	return age >= e.MaxAge
}

func finish(g model.Group, err error, now time.Time) model.Group {
	if err != nil {
		g.Status, g.Message = providers.StatusOf(err)
		return g
	}
	g.Status = model.StatusOK
	g.LastSuccess = model.T(now)
	return g
}

func (e *Engine) path(id string) string { return filepath.Join(e.CacheDir, id+".json") }

func (e *Engine) load(id string) (Entry, bool) {
	var c Entry
	data, err := os.ReadFile(e.path(id))
	if err != nil || json.Unmarshal(data, &c) != nil || c.Group.ID != id {
		return Entry{}, false
	}
	return c, true
}

func (e *Engine) save(id string, c Entry) {
	data, err := json.Marshal(c)
	if err == nil {
		_ = config.WriteFileAtomic(e.path(id), data, 0o600)
	}
}

// LastSuccess reports a provider's last good fetch from the cache (doctor).
func (e *Engine) LastSuccess(id string) *time.Time {
	c, ok := e.load(id)
	if !ok {
		return nil
	}
	return c.LastSuccess
}

// Invalidate drops a provider's cache so the next run fetches it.
func (e *Engine) Invalidate(id string) { _ = os.Remove(e.path(id)) }
