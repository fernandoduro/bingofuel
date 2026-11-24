package providers

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha1"
	"encoding/base64"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fernandoduro/bingofuel/internal/config"
	"github.com/fernandoduro/bingofuel/internal/model"
)

func claimsJWT(payload string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none"}`)) + "." + enc([]byte(payload)) + ".sig"
}

var cursorTok = claimsJWT(`{"sub":"auth0|user_ABC123","exp":9999999999}`)

// stubCursorDB answers the state.vscdb query without a real database.
func stubCursorDB(t *testing.T, value string, err error) {
	t.Helper()
	old := cursorDBQuery
	cursorDBQuery = func(ctx context.Context, db, key string) (string, error) { return value, err }
	t.Cleanup(func() { cursorDBQuery = old })
}

func TestParseCursor(t *testing.T) {
	plan, meters, details := parseCursor(fixture(t, "cursor-usage-summary.json"))
	if plan != "Pro" {
		t.Errorf("plan = %q", plan)
	}
	got := byID(meters)
	for id, w := range map[string]struct {
		label string
		pct   float64
	}{"total": {"Monthly", 75}, "auto": {"Auto", 12.5}, "api": {"API", 61.25}} {
		m, ok := got[id]
		if !ok {
			t.Fatalf("missing meter %s in %+v", id, meters)
		}
		if m.Label != w.label || m.Percent != w.pct || m.Kind != model.KindWindow || m.ResetsAt == nil || m.ResetsAt.Month() != 12 {
			t.Errorf("%s = %+v, want label %q pct %v resetting in December", id, m, w.label, w.pct)
		}
	}
	want := model.Detail{Label: "On-demand", Value: "5.00 of 100.00 USD"}
	if len(details) != 1 || details[0] != want {
		t.Errorf("details = %+v, want only %+v (included dollars read 20 of 20 while bonus credit keeps the plan at 75%%)", details, want)
	}
}

func TestParseCursorCentsFallback(t *testing.T) {
	_, meters, details := parseCursor(fixture(t, "cursor-cents-only.json"))
	if len(meters) != 1 || meters[0].ID != "total" || math.Abs(meters[0].Percent-73.84) > 1e-9 {
		t.Errorf("meters = %+v, want total 73.84 from overall cents", meters)
	}
	if len(details) != 1 || details[0].Value != "73.84 of 100.00 USD" {
		t.Errorf("details = %+v (disabled on-demand must be skipped)", details)
	}
}

func TestCursorToken(t *testing.T) {
	dir := t.TempDir()
	c := Cursor{Cfg: config.Cursor{DB: filepath.Join(dir, "state.vscdb")}, AgentAuth: filepath.Join(dir, "auth.json")}

	stubCursorDB(t, "", nil)
	if _, _, err := c.login(context.Background(), now); !errorStatus(err, model.StatusNotConfigured) {
		t.Errorf("no login anywhere: err = %v, want not_configured", err)
	}

	expired := claimsJWT(`{"sub":"auth0|user_ABC123","exp":1}`)
	stubCursorDB(t, expired, nil)
	_, _, err := c.login(context.Background(), now)
	if !errorStatus(err, model.StatusAuthExpired) || !strings.Contains(err.Error(), "open Cursor") {
		t.Errorf("expired app token: err = %v, want auth_expired telling the user to open Cursor", err)
	}
	if err != nil && strings.Contains(err.Error(), expired) {
		t.Error("error message leaks the token")
	}

	os.WriteFile(c.AgentAuth, []byte(`{"accessToken":"`+cursorTok+`"}`), 0o600)
	if cookie, _, err := c.login(context.Background(), now); err != nil || cookie != "WorkosCursorSessionToken=user_ABC123%3A%3A"+cursorTok {
		t.Errorf("agent fallback: cookie=%q err=%v", cookie, err)
	}

	stubCursorDB(t, cursorTok+"\n", nil)
	os.Remove(c.AgentAuth)
	if cookie, _, err := c.login(context.Background(), now); err != nil || !strings.HasPrefix(cookie, "WorkosCursorSessionToken=user_ABC123%3A%3A") {
		t.Errorf("app token: cookie=%q err=%v", cookie, err)
	}

	stubCursorDB(t, claimsJWT(`{"exp":9999999999}`), nil)
	if _, _, err := c.login(context.Background(), now); !errorStatus(err, model.StatusNotConfigured) {
		t.Errorf("token without sub: err = %v, want not_configured", err)
	}
}

func TestCursorFetchHTTP(t *testing.T) {
	stubCursorDB(t, cursorTok, nil)
	body, _ := os.ReadFile("testdata/cursor-usage-summary.json")
	code := 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "WorkosCursorSessionToken=user_ABC123%3A%3A"+cursorTok {
			t.Errorf("bad cookie header: %q", r.Header.Get("Cookie"))
		}
		w.WriteHeader(code)
		if code == 200 {
			w.Write(body)
		} else {
			w.Write([]byte(`{"error":"tok leaked?"}`))
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	c := Cursor{Cfg: config.Cursor{DB: filepath.Join(dir, "state.vscdb"), Meters: []string{"total"}}, AgentAuth: filepath.Join(dir, "none.json"), URL: srv.URL}

	g, err := c.Fetch(context.Background(), now)
	if err != nil || len(g.Meters) != 3 || g.Plan != "Pro" {
		t.Fatalf("fetch: %v %+v", err, g)
	}
	if s := byID(g.Meters); !s["total"].Show || s["auto"].Show || s["api"].Show {
		t.Errorf("selection not applied: %+v", g.Meters)
	}
	for _, tc := range []struct {
		code int
		want model.Status
	}{{401, model.StatusAuthExpired}, {403, model.StatusAuthExpired}, {429, model.StatusRateLimited}, {500, model.StatusStale}} {
		code = tc.code
		_, err := c.Fetch(context.Background(), now)
		if !errorStatus(err, tc.want) {
			t.Errorf("HTTP %d: err = %v, want %s", tc.code, err, tc.want)
		}
		if err != nil && strings.Contains(err.Error(), "leaked") {
			t.Errorf("HTTP %d: error echoes the body", tc.code)
		}
	}

	code = 200
	body = []byte(`{"membershipType":"free","individualUsage":{}}`)
	if _, err := c.Fetch(context.Background(), now); !errorStatus(err, model.StatusPlanLapsed) {
		t.Errorf("no usage figures: err = %v, want plan_lapsed", err)
	}
}

func safeStorageEncrypt(t *testing.T, version, password, plain string) string {
	t.Helper()
	key, _ := pbkdf2.Key(sha1.New, password, []byte("saltysalt"), 1, 16)
	block, _ := aes.NewCipher(key)
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	in := append([]byte(plain), bytes.Repeat([]byte{byte(pad)}, pad)...)
	out := make([]byte, len(in))
	cipher.NewCBCEncrypter(block, bytes.Repeat([]byte(" "), aes.BlockSize)).CryptBlocks(out, in)
	return cursorSafePrefix + base64.StdEncoding.EncodeToString(append([]byte(version), out...))
}

func TestCursorSafeStorageToken(t *testing.T) {
	old := safeStoragePassword
	t.Cleanup(func() { safeStoragePassword = old })
	safeStoragePassword = func(context.Context) (string, error) { return "keyring-pw", nil }
	ctx := context.Background()

	if got, err := decryptCursorToken(ctx, safeStorageEncrypt(t, "v11", "keyring-pw", cursorTok)); err != nil || got != cursorTok {
		t.Errorf("v11: got %q err %v", got, err)
	}
	if got, err := decryptCursorToken(ctx, safeStorageEncrypt(t, "v10", "peanuts", cursorTok)); err != nil || got != cursorTok {
		t.Errorf("v10: got %q err %v", got, err)
	}
	if _, err := decryptCursorToken(ctx, safeStorageEncrypt(t, "v11", "wrong-pw", cursorTok)); !errorStatus(err, model.StatusNotConfigured) {
		t.Errorf("wrong key: err = %v, want not_configured", err)
	}
	if got, _ := decryptCursorToken(ctx, cursorTok); got != cursorTok {
		t.Errorf("plain token changed: %q", got)
	}

	stubCursorDB(t, safeStorageEncrypt(t, "v11", "keyring-pw", cursorTok), nil)
	dir := t.TempDir()
	c := Cursor{Cfg: config.Cursor{DB: filepath.Join(dir, "state.vscdb")}, AgentAuth: filepath.Join(dir, "none.json")}
	if cookie, _, err := c.login(ctx, now); err != nil || cookie != "WorkosCursorSessionToken=user_ABC123%3A%3A"+cursorTok {
		t.Errorf("encrypted app token: cookie=%q err=%v", cookie, err)
	}
}
