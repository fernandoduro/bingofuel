# Bingo Fuel

In aviation, *bingo fuel* is the fuel level at which a pilot has to turn for home or won't make it back. Bingo Fuel is the same warning for AI work: rings on your desktop that show how much of your Claude, Codex and Cursor plan limits you've used, and how much prepaid OpenRouter and JEV credit is left, before you start a task you can't finish.

![Bingo Fuel rings on the desktop](docs/screenshots/rings.png)

Single-row layout, and the popup with every limit, exact reset times and the JEV balance field:

![Single-row layout](docs/screenshots/rings-row.png)

<img src="docs/screenshots/popup.png" alt="Popup" width="424">

- **Quota rings** (Claude, Codex, Cursor) show the share of the window used, its name and the time until it resets. Past the warning threshold the arc and the name take the warning colour.
- **Credit rings** (OpenRouter, JEV) fill with the share of the credit spent, and show only what's left.
- One poll cycle for every provider, with a cache: a 5-minute floor between calls, the last figures held for 30 minutes after a rate limit, and a refetch when a window resets.
- Failures never show as 0%. A provider whose data is old or whose login expired is dimmed, with the reason on hover.

## What it reads, and what it never does

| Provider | Source | Notes |
|---|---|---|
| Claude | `~/.claude/.credentials.json` (or `$CLAUDE_CONFIG_DIR`); `GET api.anthropic.com/api/oauth/usage` | The same usage endpoint Claude Code's `/usage` reads. Undocumented. |
| Codex | `~/.codex/auth.json` (or `$CODEX_HOME`); `GET chatgpt.com/backend-api/wham/usage` | Undocumented. Needs a ChatGPT login, not API-key mode. |
| Cursor | Cursor's `state.vscdb` (`cursorAuth/accessToken`, read with the `sqlite3` CLI), then cursor-agent's `auth.json`; `GET cursor.com/api/usage-summary` | Undocumented; the dashboard's own endpoint. Cursor 3.x encrypts the token with Electron safeStorage: on Linux the key is read from KWallet (`Chromium Keys`) or, untested so far, the Secret Service; it is used in memory and never stored. macOS encrypted tokens are not supported yet. That key is shared by every Chromium-based app on the desktop. |
| OpenRouter | API key; `GET /api/v1/credits` and `/api/v1/key` | Documented. |
| JEV (TypeSafe) | A balance you type in; JEV calls found in local usage logs | No documented API balance endpoint has been found; this figure is calculated locally, with that distinction explained in the tooltip and popup. |

- **Read-only.** It never refreshes or rewrites another tool's login: refresh tokens rotate, and refreshing behind the CLI's back can log it out. An expired login shows "run claude to refresh it" (or "open Cursor to sign in again").
- **No usage quota spent.** These endpoints report usage; they don't call a model.
- **Nothing leaves the machine** except those requests. API keys go in the OS keyring (Secret Service, macOS Keychain, Windows Credential Manager), or a 0600 file when no keyring is reachable.
- **Allow-listed output.** Provider payloads carry personal fields (Codex includes your email and account ids); only usage figures reach the cache and the widget.

## Install (Arch / KDE Plasma 6)

Needs Go 1.24+ and Plasma 6.

```bash
make install          # bingofuel -> ~/.local/bin, widget -> your Plasma applets
bingofuel setup       # choose providers and rings, store keys
bingofuel doctor      # check everything
```

Then right-click the desktop → **Add Widgets…** → **Bingo Fuel**. Colours, thresholds and layout are in the widget's settings.

## Commands

```
bingofuel fetch [--max-age S] [--pretty]   JSON for front-ends (always exits 0)
bingofuel status                           terminal summary
bingofuel setup                            interactive setup
bingofuel doctor                           check every provider and explain problems
bingofuel jev balance <USD>                record the TypeSafe balance after a top-up
bingofuel secret set <openrouter|typesafe> store a key read from stdin
```

## JEV balance

TypeSafe's API has no balance endpoint. Bingo Fuel keeps a small ledger instead:

1. Enter the balance shown at console.typesafe.ai after each top-up: in `bingofuel setup`, with `bingofuel jev balance 4.98`, or in the widget's popup.
2. It subtracts `input_tokens × $0.042 / M` for every JEV call in the usage logs you list (the Hermes Agent delegation-tiers log is detected automatically; any JSONL with `ts` and `input_tokens` works).
3. A billing error (HTTP 402) in a log sets the balance to $0.

## Layout

```
cmd/bingofuel/          CLI entry point
internal/providers/     one file per provider; lenient, allow-listed parsers
internal/engine/        fetch-or-serve decisions and the cache
internal/cli/           fetch, status, setup wizard, doctor
frontends/plasma/       the KDE Plasma 6 widget (draws only; all logic is in the core)
```

`make test` runs the Go tests and the QML tests. `frontends/plasma/tools/preview.qml` and `preview-popup.qml` render the widget offscreen from `bingofuel fetch` output; the screenshots above come from them. Other desktops (GNOME, macOS, Windows) can be added as further front-ends reading `bingofuel fetch --json`.

## Credits

The ring gauge with the value, window name and countdown inside grew out of [p3kj/plasma-applet-claudemeter](https://github.com/p3kj/plasma-applet-claudemeter) (MIT). [Arzaroth/TokenGauge](https://github.com/Arzaroth/TokenGauge) documented many of the endpoint pitfalls this project designs around.

MIT licence.
