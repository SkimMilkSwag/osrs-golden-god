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
| `ledger.go` | `Ledger` / `Entry` / `Summarize` — append-only event log with strictly-increasing-tick enforcement and a kind-based summary (clicks, keypresses, banks, chats, session count, time span). |

The actual client integration (RuneLite plugin vs. vision bot) is deliberately
kept out of this repo for now — the behavior layer is where the experiment
lives, and it should be swappable.

## Running

```
go test ./...
go run . plan --min-hours 2 --max-hours 6 --sessions 3
go run . demo   # prints a synthetic day: planned sessions + a sample ledger summary
```

`plan` is deterministic for a fixed session-length list; `demo` uses a seeded
RNG so its output is stable across runs (handy for diffing after a change).

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

## Testing

Pure Go, stdlib only: `go test ./...`. Tests pin the planning math (action
estimates under the efficiency factor, budget exhaustion, error paths), the
planner's no-repeat rule (with a seeded RNG), and the ledger's tick
monotonicity + summary counts.

## Status / next

- [ ] Wire `PlanDay` output to the client layer (RuneLite agent-server or vision bot)
- [ ] Reflex layer for combat (prayer/potion/death rules)
- [ ] JSONL file writer behind the ledger interface
- [ ] One live week, then compare Botwatch outcomes against the knobs

## License

[MIT](LICENSE) — same as the rest of the portfolio.
