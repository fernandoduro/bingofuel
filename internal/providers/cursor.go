package providers

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fernandoduro/bingofuel/internal/config"
	"github.com/fernandoduro/bingofuel/internal/model"
)

const (
	cursorUsageURL = "https://cursor.com/api/usage-summary"
	cursorTokenKey = "cursorAuth/accessToken"
	// Tokens closer than this to expiry are treated as expired.
	cursorExpirySkew = time.Minute
	// Cursor 3.x wraps its token in Electron safeStorage ciphertext.
	cursorSafePrefix = "cursor-safe-storage:v1:"
	// Electron's fixed key for "v10" blobs when no keyring was available.
	safeStorageFallbackPassword = "peanuts"
)

type Cursor struct {
	Cfg config.Cursor
	// AgentAuth is cursor-agent's auth.json; empty uses the default location.
	AgentAuth string
	// URL overrides the endpoint in tests.
	URL string
}

func (Cursor) ID() string            { return "cursor" }
func (Cursor) Title() string         { return "Cursor" }
func (Cursor) Color() string         { return "#3CCFB4" }
func (Cursor) Local() bool           { return false }
func (c Cursor) Selection() []string { return c.Cfg.Meters }

func (c Cursor) db() string {
	if c.Cfg.DB != "" {
		return config.Expand(c.Cfg.DB)
	}
	base, _ := os.UserConfigDir()
	return filepath.Join(base, "Cursor", "User", "globalStorage", "state.vscdb")
}

func (c Cursor) agentAuth() string {
	if c.AgentAuth != "" {
		return c.AgentAuth
	}
	base, _ := os.UserConfigDir()
	return filepath.Join(base, "cursor", "auth.json")
}

// cursorDBQuery reads one ItemTable value with the sqlite3 CLI, read-only.
// The token comes back on a pipe; it never enters argv.
var cursorDBQuery = func(ctx context.Context, db, key string) (string, error) {
	if _, err := os.Stat(db); err != nil {
		return "", nil
	}
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		return "", errf(model.StatusNotConfigured, "reading Cursor's login needs the sqlite3 command")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-readonly", "-batch", "-noheader", db,
		"SELECT value FROM ItemTable WHERE key = '"+key+"'").Output()
	if err != nil {
		return "", errf(model.StatusStale, "could not read %s", db)
	}
	return strings.TrimSpace(string(out)), nil
}

// safeStoragePassword fetches the Linux Electron safeStorage key: KWallet on
// KDE, then the Secret Service on GNOME. It is read on a pipe, never stored.
var safeStoragePassword = func(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmds := [][]string{
		{"kwallet-query", "-r", "Chromium Safe Storage", "-f", "Chromium Keys", "kdewallet"},
		{"secret-tool", "lookup", "xdg:schema", "chrome_libsecret_os_crypt_password_v2", "application", "cursor"},
		{"secret-tool", "lookup", "xdg:schema", "chrome_libsecret_os_crypt_password_v2", "application", "Cursor"},
	}
	for _, c := range cmds {
		bin, err := exec.LookPath(c[0])
		if err != nil {
			continue
		}
		out, err := exec.CommandContext(ctx, bin, c[1:]...).Output()
		if pw := strings.TrimRight(string(out), "\n"); err == nil && pw != "" {
			return pw, nil
		}
	}
	return "", errf(model.StatusNotConfigured, "Cursor's login is encrypted and its key was not found in KWallet or the Secret Service")
}

// decryptCursorToken returns the plain JWT. Electron safeStorage on Linux:
// PBKDF2-SHA1(password, "saltysalt", 1 round, 16 bytes), AES-128-CBC with an
// IV of 16 spaces; "v10" uses the fixed password, "v11" the keyring's.
func decryptCursorToken(ctx context.Context, v string) (string, error) {
	if !strings.HasPrefix(v, cursorSafePrefix) {
		return v, nil
	}
	undecryptable := errf(model.StatusNotConfigured, "could not decrypt Cursor's stored login")
	blob, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(v, cursorSafePrefix))
	if err != nil || len(blob) <= 3 || (len(blob)-3)%aes.BlockSize != 0 {
		return "", undecryptable
	}
	var pw string
	switch string(blob[:3]) {
	case "v10":
		pw = safeStorageFallbackPassword
	case "v11":
		if pw, err = safeStoragePassword(ctx); err != nil {
			return "", err
		}
	default:
		return "", undecryptable
	}
	key, err := pbkdf2.Key(sha1.New, pw, []byte("saltysalt"), 1, 16)
	if err != nil {
		return "", undecryptable
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", undecryptable
	}
	out := make([]byte, len(blob)-3)
	cipher.NewCBCDecrypter(block, bytes.Repeat([]byte(" "), aes.BlockSize)).CryptBlocks(out, blob[3:])
	pad := int(out[len(out)-1])
	if pad == 0 || pad > aes.BlockSize || !bytes.Equal(out[len(out)-pad:], bytes.Repeat([]byte{byte(pad)}, pad)) {
		return "", undecryptable
	}
	return string(out[:len(out)-pad]), nil
}

// CredentialSource describes the first usable login, without the token.
func (c Cursor) CredentialSource() (string, error) {
	_, src, err := c.login(context.Background(), time.Now())
	return src, err
}

// login returns the cookie cursor.com expects, built from the first usable
// token: the Cursor app's, then cursor-agent's. Neither is ever refreshed.
func (c Cursor) login(ctx context.Context, now time.Time) (string, string, error) {
	type cand struct {
		name string
		read func() (string, error)
	}
	cands := []cand{
		{c.db(), func() (string, error) {
			v, err := cursorDBQuery(ctx, c.db(), cursorTokenKey)
			if err != nil || v == "" {
				return "", err
			}
			return decryptCursorToken(ctx, v)
		}},
		{c.agentAuth(), func() (string, error) {
			data, err := os.ReadFile(c.agentAuth())
			if err != nil {
				return "", nil
			}
			var a struct {
				AccessToken string `json:"accessToken"`
			}
			_ = json.Unmarshal(data, &a)
			return a.AccessToken, nil
		}},
	}
	var expired string
	var firstErr error
	for _, cd := range cands {
		tok, err := cd.read()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		claims, ok := jwtClaims(tok)
		if !ok {
			continue // empty, encrypted (macOS safe storage) or not a JWT
		}
		if exp, _ := num(claims["exp"]); exp > 0 && now.Add(cursorExpirySkew).After(time.Unix(int64(exp), 0)) {
			expired = cd.name
			continue
		}
		sub := str(claims["sub"])
		userID := sub[strings.LastIndex(sub, "|")+1:]
		if userID == "" {
			continue
		}
		return "WorkosCursorSessionToken=" + userID + "%3A%3A" + tok, cd.name, nil
	}
	if expired != "" {
		return "", expired, errf(model.StatusAuthExpired, "Cursor login expired: open Cursor to sign in again")
	}
	if firstErr != nil {
		return "", "", firstErr
	}
	return "", "", errf(model.StatusNotConfigured, "no Cursor login found in %s or %s", c.db(), c.agentAuth())
}

// jwtClaims decodes the payload without verifying the signature.
func jwtClaims(tok string) (map[string]any, bool) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return nil, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	var claims map[string]any
	if json.Unmarshal(payload, &claims) != nil || claims == nil {
		return nil, false
	}
	return claims, true
}

func (c Cursor) Fetch(ctx context.Context, now time.Time) (model.Group, error) {
	g := model.Group{ID: c.ID(), Title: c.Title(), Color: c.Color()}
	cookie, _, err := c.login(ctx, now)
	if err != nil {
		return g, err
	}
	url := c.URL
	if url == "" {
		url = cursorUsageURL
	}
	body, _, err := get(ctx, url, map[string]string{"Cookie": cookie}, "Cursor")
	if err != nil {
		// cursor.com answers a rejected session with either 401 or 403.
		var pe *Error
		if errors.As(err, &pe) && (pe.Status == model.StatusAuthExpired || pe.Status == model.StatusForbidden) {
			return g, errf(model.StatusAuthExpired, "Cursor rejected the login: open Cursor to sign in again")
		}
		return g, err
	}
	m, err := decode(body, "Cursor")
	if err != nil {
		return g, err
	}
	g.Plan, g.Meters, g.Details = parseCursor(m)
	if len(g.Meters) == 0 {
		return g, errf(model.StatusPlanLapsed, "Cursor reported no plan usage: is the subscription still active?")
	}
	applySelection(g.Meters, c.Cfg.Meters)
	return g, nil
}

// parseCursor reads usage-summary. Amounts are in cents; the *PercentUsed
// fields are already percentages, even when below 1.
func parseCursor(m map[string]any) (string, []model.Meter, []model.Detail) {
	resets := parseTime(m["billingCycleEnd"])
	ind := obj(m["individualUsage"])
	plan := obj(ind["plan"])

	var meters []model.Meter
	add := func(id, label string, pct float64) {
		meters = append(meters, model.Meter{ID: id, Kind: model.KindWindow, Label: label, Percent: clampPct(pct), ResetsAt: resets})
	}
	cents := func(o map[string]any) (used, limit float64, ok bool) {
		used, uok := num(o["used"])
		limit, lok := num(o["limit"])
		return used, limit, uok && lok && limit > 0
	}

	if pct, ok := num(plan["totalPercentUsed"]); ok {
		add("total", "Monthly", pct)
	} else if used, limit, ok := cents(plan); ok {
		add("total", "Monthly", used/limit*100)
	} else if used, limit, ok := cents(obj(ind["overall"])); ok {
		add("total", "Monthly", used/limit*100)
	}
	if pct, ok := num(plan["autoPercentUsed"]); ok {
		add("auto", "Auto", pct)
	}
	if pct, ok := num(plan["apiPercentUsed"]); ok {
		add("api", "API", pct)
	}

	var details []model.Detail
	usd := func(label string, o map[string]any) {
		if used, limit, ok := cents(o); ok {
			details = append(details, model.Detail{Label: label, Value: fmt.Sprintf("%.2f of %.2f USD", used/100, limit/100)})
		}
	}
	// With bonus credit, plan.used can equal plan.limit while the percentages
	// are low; the dollar figure is shown only when it drove the ring.
	if _, ok := num(plan["totalPercentUsed"]); !ok {
		if _, _, ok := cents(plan); ok {
			usd("Included", plan)
		} else {
			usd("Included", obj(ind["overall"]))
		}
	}
	if od := obj(ind["onDemand"]); od != nil {
		if on, _ := od["enabled"].(bool); on {
			usd("On-demand", od)
		}
	}
	return titleWords(str(m["membershipType"])), meters, details
}
