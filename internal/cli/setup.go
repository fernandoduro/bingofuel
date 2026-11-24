package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/fernandoduro/bingofuel/internal/config"
	"github.com/fernandoduro/bingofuel/internal/engine"
	"github.com/fernandoduro/bingofuel/internal/model"
	"github.com/fernandoduro/bingofuel/internal/providers"
	"github.com/fernandoduro/bingofuel/internal/secrets"
)

func cmdSetup(_ []string) int {
	if !isTerminal(os.Stdin) {
		fmt.Fprintln(os.Stderr, "setup needs an interactive terminal")
		return 2
	}
	if err := setup(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			fmt.Println("Setup cancelled. Keys or balance checkpoints already saved were kept.")
			return 1
		}
		fmt.Fprintln(os.Stderr, "setup failed:", err)
		return 1
	}
	return 0
}

func setup() error {
	cfg, err := config.Load()
	if err != nil {
		fmt.Println("Existing config is unreadable; starting from defaults:", err)
		cfg = config.Defaults()
	}
	now := time.Now()

	// 1. Detect.
	_, claudeErr := (providers.Claude{Cfg: cfg.Claude}).CredentialSource()
	_, codexErr := (providers.Codex{Cfg: cfg.Codex}).CredentialSource()
	_, cursorErr := (providers.Cursor{Cfg: cfg.Cursor}).CredentialSource()
	_, orSrc, _ := secrets.Get("openrouter", cfg.OpenRouter.KeyEnv)
	_, tsSrc, _ := secrets.Get("typesafe", cfg.TypeSafe.KeyEnv)
	found := func(err error) string {
		if err == nil {
			return "found"
		}
		return "not found"
	}
	keyFound := func(s secrets.Source) string {
		if s == secrets.SourceNone {
			return "no key yet"
		}
		return "key in " + describeSource(s)
	}
	intro := fmt.Sprintf("Detected on this machine:\n\n  Claude      login %s\n  Codex       login %s\n  Cursor      login %s\n  OpenRouter  %s\n  JEV         %s\n\nKeys are stored in your OS keyring and never printed.",
		found(claudeErr), found(codexErr), found(cursorErr), keyFound(orSrc), keyFound(tsSrc))

	enabled := []string{}
	if !cfg.Found {
		if claudeErr == nil || !errorIs(claudeErr, model.StatusNotConfigured) {
			enabled = append(enabled, "claude")
		}
		if codexErr == nil || !errorIs(codexErr, model.StatusNotConfigured) {
			enabled = append(enabled, "codex")
		}
		if cursorErr == nil || !errorIs(cursorErr, model.StatusNotConfigured) {
			enabled = append(enabled, "cursor")
		}
		if orSrc != secrets.SourceNone {
			enabled = append(enabled, "openrouter")
		}
		if tsSrc != secrets.SourceNone {
			enabled = append(enabled, "typesafe")
		}
	} else {
		for id, on := range map[string]bool{"claude": cfg.Claude.Enabled, "codex": cfg.Codex.Enabled, "cursor": cfg.Cursor.Enabled, "openrouter": cfg.OpenRouter.Enabled, "typesafe": cfg.TypeSafe.Enabled} {
			if on {
				enabled = append(enabled, id)
			}
		}
	}

	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().Title("Bingo Fuel setup").Description(intro),
			huh.NewMultiSelect[string]().Title("Which providers should the widget show?").
				Options(
					huh.NewOption("Claude (subscription limits)", "claude"),
					huh.NewOption("Codex (subscription limits)", "codex"),
					huh.NewOption("Cursor (monthly plan usage)", "cursor"),
					huh.NewOption("OpenRouter (prepaid credits)", "openrouter"),
					huh.NewOption("JEV by TypeSafe (prepaid credits, estimated)", "typesafe"),
				).Value(&enabled),
		),
	).Run(); err != nil {
		return err
	}
	on := map[string]bool{}
	for _, id := range enabled {
		on[id] = true
	}
	cfg.Claude.Enabled, cfg.Codex.Enabled, cfg.Cursor.Enabled, cfg.OpenRouter.Enabled, cfg.TypeSafe.Enabled = on["claude"], on["codex"], on["cursor"], on["openrouter"], on["typesafe"]

	// 2. Per-provider details.
	if on["claude"] {
		meters := cfg.Claude.Meters
		if len(meters) == 0 {
			meters = []string{"session", "weekly"}
		}
		if err := huh.NewMultiSelect[string]().Title("Claude rings").
			Description("Rings appear only for what you pick. The top-model ring shows the most-used model-specific weekly limit, when Claude reports one.").
			Options(
				huh.NewOption("5-Hour", "session"),
				huh.NewOption("Weekly (all models)", "weekly"),
				huh.NewOption("Weekly (top model)", "model_weekly"),
			).Value(&meters).Validate(atLeastOne).Run(); err != nil {
			return err
		}
		cfg.Claude.Meters = meters
	}
	if on["codex"] {
		all := len(cfg.Codex.Meters) == 0
		if err := huh.NewConfirm().Title("Show every window Codex reports as a ring?").
			Description("Usually 5-Hour and Weekly. Choose No to pick.").Value(&all).Run(); err != nil {
			return err
		}
		if all {
			cfg.Codex.Meters = nil
		} else {
			meters := cfg.Codex.Meters
			if err := huh.NewMultiSelect[string]().Title("Codex rings").Options(
				huh.NewOption("5-Hour", "session"),
				huh.NewOption("Weekly", "weekly"),
				huh.NewOption("Monthly (free plan)", "monthly"),
			).Value(&meters).Validate(atLeastOne).Run(); err != nil {
				return err
			}
			cfg.Codex.Meters = meters
		}
	}
	if on["cursor"] {
		meters := cfg.Cursor.Meters
		if len(meters) == 0 {
			meters = []string{"total", "auto", "api"}
		}
		if err := huh.NewMultiSelect[string]().Title("Cursor rings").
			Description("Auto and API split the monthly usage by model pool, on plans that report them.").
			Options(
				huh.NewOption("Monthly (total)", "total"),
				huh.NewOption("Auto + Composer", "auto"),
				huh.NewOption("API models", "api"),
			).Value(&meters).Validate(atLeastOne).Run(); err != nil {
			return err
		}
		cfg.Cursor.Meters = meters
	}
	if on["openrouter"] {
		if err := setupKey("openrouter", "OpenRouter", cfg.OpenRouter.KeyEnv, orSrc, func(ctx context.Context, key string) (string, error) {
			g, err := providers.OpenRouter{Cfg: cfg.OpenRouter, Key: key}.Fetch(ctx, time.Now())
			if err != nil {
				return "", err
			}
			return summarize(g), nil
		}); errors.Is(err, errKeySkipped) {
			cfg.OpenRouter.Enabled = false
			fmt.Println("  OpenRouter disabled; run setup again with a working key.")
		} else if err != nil {
			return err
		}
	}
	if on["typesafe"] {
		if err := setupKey("typesafe", "TypeSafe", cfg.TypeSafe.KeyEnv, tsSrc, func(ctx context.Context, key string) (string, error) {
			if err := providers.CheckTypeSafeKey(ctx, key); err != nil {
				return "", err
			}
			return "key accepted", nil
		}); errors.Is(err, errKeySkipped) {
			// The JEV ring is a local estimate; it still works without a valid key.
			fmt.Println("  Key not stored; the JEV estimate works without it.")
		} else if err != nil {
			return err
		}
		if err := setupJEV(&cfg, now); err != nil {
			return err
		}
	}

	// 3. Save.
	if err := config.Save(cfg); err != nil {
		return err
	}
	if e, err := engine.New(cfg); err == nil {
		for _, id := range []string{"claude", "codex", "cursor", "openrouter"} {
			e.Invalidate(id) // a new selection or key should show at once
		}
	}
	path := cfg.Path
	if path == "" {
		d, _ := config.Dir()
		path = d + "/config.toml"
	}
	fmt.Printf("\nSaved %s\n\n", path)
	out, _ := run(-1)
	renderStatus(os.Stdout, out, time.Now(), isTerminal(os.Stdout) && os.Getenv("NO_COLOR") == "")
	fmt.Println("\nColours, thresholds and layout are in the widget's own settings. `bingofuel doctor` checks everything again.")
	return nil
}

// An empty selection means "every meter", so the wizard asks for one or more.
func atLeastOne(v []string) error {
	if len(v) == 0 {
		return errors.New("pick at least one ring")
	}
	return nil
}

func errorIs(err error, s model.Status) bool {
	st, _ := providers.StatusOf(err)
	return st == s
}

var errKeySkipped = errors.New("key not stored")

func setupKey(id, name, env string, existing secrets.Source, test func(context.Context, string) (string, error)) error {
	const keep, fromEnv, paste = "keep", "env", "paste"
	choice := paste
	opts := []huh.Option[string]{}
	if existing == secrets.SourceKeyring || existing == secrets.SourceFile {
		opts = append(opts, huh.NewOption("Keep the stored key", keep))
		choice = keep
	}
	if env != "" && os.Getenv(env) != "" {
		opts = append(opts, huh.NewOption("Import from $"+env, fromEnv))
		if choice == paste {
			choice = fromEnv
		}
	}
	opts = append(opts, huh.NewOption("Paste a key", paste))
	if len(opts) > 1 {
		if err := huh.NewSelect[string]().Title(name + " API key").Options(opts...).Value(&choice).Run(); err != nil {
			return err
		}
	}
	var key string
	switch choice {
	case keep:
		k, _, err := secrets.Get(id, "")
		if err != nil {
			return err
		}
		key = k
	case fromEnv:
		key = os.Getenv(env)
	default:
		if err := huh.NewInput().Title(name + " API key").Description("Input is hidden.").
			EchoMode(huh.EchoModePassword).Value(&key).
			Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return errors.New("required")
				}
				return nil
			}).Run(); err != nil {
			return err
		}
		key = strings.TrimSpace(key)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if res, err := test(ctx, key); err != nil {
		status, msg := providers.StatusOf(err)
		fmt.Printf("  ✗ %s: %s\n", name, msg)
		if choice == keep {
			fmt.Println("  Keeping the stored key; fix it with `bingofuel secret set " + id + "`.")
			return nil
		}
		// A key the provider rejected defaults to No; a network blip defaults to Yes.
		store := status != model.StatusAuthExpired && status != model.StatusForbidden
		if err := huh.NewConfirm().Title("Store this key anyway?").Value(&store).Run(); err != nil {
			return err
		}
		if !store {
			return errKeySkipped
		}
	} else {
		fmt.Printf("  ✓ %s: %s\n", name, res)
	}
	if choice == keep {
		return nil
	}
	src, err := secrets.Set(id, key)
	if err != nil {
		return err
	}
	fmt.Printf("  %s key stored in the %s.\n", name, describeSource(src))
	return nil
}

func setupJEV(cfg *config.Config, now time.Time) error {
	ts := providers.TypeSafe{Cfg: cfg.TypeSafe}
	l, _ := ts.LoadLedger()
	desc := "Read it at console.typesafe.ai/settings/billing. TypeSafe has no balance API, so Bingo Fuel subtracts the JEV calls it finds in your usage logs from this figure."
	if n := len(l.Checkpoints); n > 0 {
		c := l.Checkpoints[n-1]
		desc += fmt.Sprintf("\nLast entered: $%.2f on %s. Leave empty to keep it.", c.BalanceUSD, c.At.Local().Format("2006-01-02"))
	}
	var bal string
	logs := strings.Join(cfg.TypeSafe.UsageLogs, ", ")
	if logs == "" {
		if h, err := os.UserHomeDir(); err == nil {
			for _, p := range []string{
				h + "/.System/data/hermes/plugin-data/delegation-tiers/decisions.jsonl",
				h + "/.hermes/plugin-data/delegation-tiers/decisions.jsonl",
			} {
				if _, err := os.Stat(p); err == nil {
					logs = p
					break
				}
			}
		}
	}
	if err := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Current TypeSafe balance (USD)").Description(desc).Value(&bal).
			Validate(func(s string) error {
				s = strings.TrimPrefix(strings.TrimSpace(s), "$")
				if s == "" && len(l.Checkpoints) > 0 {
					return nil
				}
				if _, err := parseUSD(s); err != nil {
					return errors.New("enter an amount like 4.98")
				}
				return nil
			}),
		huh.NewInput().Title("Usage logs (comma-separated JSONL paths)").
			Description("Files that record JEV calls with input_tokens. The Hermes delegation-tiers log is detected automatically.").
			Value(&logs),
	)).Run(); err != nil {
		return err
	}
	cfg.TypeSafe.UsageLogs = nil
	for _, p := range strings.Split(logs, ",") {
		if p = strings.TrimSpace(p); p != "" {
			cfg.TypeSafe.UsageLogs = append(cfg.TypeSafe.UsageLogs, p)
		}
	}
	if s := strings.TrimSpace(bal); s != "" {
		v, err := parseUSD(s)
		if err != nil {
			return err
		}
		if err := ts.SetBalance(v, now); err != nil {
			return err
		}
		fmt.Printf("  ✓ JEV balance recorded: $%.2f\n", v)
	}
	return nil
}
