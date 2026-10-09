# OSRS Golden God

Public code for a small [Old School RuneScape](https://oldschool.runescape.com/) bot
experiment. The goal isn't the gold — it's a minimal, readable harness for
running a bot under the current (post-Jan-2026) detection landscape and
seeing which of the classic anti-ban knobs actually move the needle.

## Why a new bot in 2026?

Jagex retired the Legacy Java Client on Jan 28, 2026, which killed most
injection/reflection bots overnight. What's left that works: RuneLite plugins,
native-client vision bots, packet bots, color bots. All of them share one
property — **detection is overwhelmingly behavioral (server-side Botwatch ML),
not client scanning**. The client you run matters less than *how* you behave
on it. That means the interesting code is the behavior layer: session length,
click timing distribution, action volume, personality actions (bank, chat,
stats-tab glances). This repo is that layer, written to be testable without a
game client attached.

## Layout

| File | What's in it |
|------|--------------|
| `bot.go` | `Config` + validation, `Session`, `RunResult`, `PlanDay` — the core planning math (session lengths → action estimates under the 85% efficiency rule, with an optional daily action budget). |
| `planner.go` | `SessionPlanner` — draws session durations from the configured range, never repeating the previous one (the "never identical twice" rule). RNG is injectable for tests. |
| `ledger.go` | `Ledger` / `Entry` / `Summarize` — append-only event log with strictly-increasing-tick enforcement and a kind-based summary (clicks, keypresses, banks, chats, stats-tab glances, session count, time span). |
| `ledger_jsonl.go` | `JSONLLedger` — the ledger backed by an append-only `.jsonl` file: one compact JSON line per entry (the pinned `tick`/`kind`/`detail` wire format), `ReadJSONL` validates on-disk monotonicity and hands back the last tick for reseeding. |
| `personality.go` | `PersonalityGenerator` — schedules the "human" noise: stats-tab glances, bank visits, chat messages, each drawn at Gaussian intervals (a uniform random delay is itself a fingerprint). Deterministic for an injected RNG; `Schedule()` returns every event that fits inside a session. |
| `reflex.go` | `Reflex` — the fast rule-based layer under the planner: prayer on/off thresholds, potion drinking with heal variance and a cooldown, death + respawn. Deterministic for a given RNG seed; `State()` snapshots it for logging. |
| `internal/agent/` | Client-layer adapter: JSON-over-stdio wire protocol (`Msg` envelope), and `Server.Run` — the executor that drives a game client through planned sessions (hello handshake, keepalive pings at session boundaries, confirmed session-ends) while interleaving personality events and feeding everything to the ledger. |

The client integration (RuneLite agent-server over stdio) lives in
`internal/agent/`: the behavior layer stays in `core`, the adapter only
executes — so swapping clients never touches the planning math.

## Running

```
go test ./...
go run . plan --min-hours 2 --max-hours 6 --sessions 3
go run . demo   # prints a synthetic day: planned sessions + a sample ledger summary
go run . habits # prints a personality-actions schedule for a 1h session
```

`plan` is deterministic for a fixed session-length list; `demo` uses a seeded
RNG so its output is stable across runs (handy for diffing after a change);
`habits` takes mean intervals (`--glance-every`, `--bank-every`,
`--chat-every`) and prints the scheduled human actions for an hour.

## The anti-ban playbook, condensed

Full reasoning lives in the private research notes; the knobs that made it
into `Config`:

- **Session length** — strongest single ban predictor. Drawn per-session from
  `[MinSessionHours, MaxSessionHours]`, never identical twice (`planner.go`).
- **85% rule** — tick-perfect for hours is an anomaly. `Efficiency` scales the
  action estimate below theoretical max.
- **Gaussian click jitter** — uniform-random delay is a fingerprint.
  `ClickSigmaMs` is the σ of the timing noise; the sampler itself is in the
  client layer, but the knob lives here so the config and the behavior stay
  in one place.
- **Daily action budget** — caps total volume independent of session count,
  so a long session can't quietly blow the day's number.
- **Personality actions** — a bot that only ever performs its task is a
  fingerprint. `personality.go` schedules stats-tab glances, bank visits and
  chat at Gaussian intervals (uniform random delay is itself a distinctive
  behavioral pattern); the agent-server executor interleaves them into each
  session's action stream.

## Testing

Pure Go, stdlib only: `go test ./...`. Tests pin the planning math (action
estimates under the efficiency factor, budget exhaustion, error paths), the
planner's no-repeat rule (with a seeded RNG), the ledger's tick monotonicity
+ summary counts (including JSONL round-trips through a real file), and the
personality generator (seeded determinism, class interleaving, schedule
bounds, direct loggability into the ledger). The agent layer's tests drive a
scriptable wire peer and assert exact conversation shape: handshake,
keepalive pings at session boundaries, confirmed session-ends, mid-day drop
recovery with the partial ledger intact, and personality events appearing on
both the wire and the ledger.

## Status / next

- [x] Wire `PlanDay` output to the client layer (RuneLite agent-server over stdio) — `internal/agent/`
- [x] Reflex layer for combat (prayer/potion/death rules) — see `reflex.go`
- [x] JSONL file writer behind the ledger interface — see `core/ledger_jsonl.go`
- [x] Personality-actions generator (glances/bank/chat at Gaussian intervals, interleaved by the executor) — `core/personality.go`
- [ ] One live week, then compare Botwatch outcomes against the knobs

## License

[MIT](LICENSE) — same as the rest of the portfolio.
