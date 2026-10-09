package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"

	"github.com/SkimMilkSwag/osrs-golden-god/core"
)

// main is the thin CLI wrapper over the behavior layer. The subcommands
// today:
//
//	osrs-golden-god plan --min-hours 2 --max-hours 6 --sessions 3 [--seed N]
//	    Plans N sessions from the given range and prints a RunResult summary.
//	    Deterministic for a fixed --seed (defaults to 1).
//
//	osrs-golden-god demo
//	    Prints a synthetic day: three planned sessions plus a sample ledger
//	    summary, so a fresh clone has something visible without a client.
//
//	osrs-golden-god habits [--glance-every S] [--bank-every S] [--chat-every S]
//	    Prints a personality-actions schedule (stats-tab glances, bank visits,
//	    chat) for a 1h session at the given mean intervals (defaults: 300/900/
//	    1200s). The Gaussian jitter is internal; output is illustrative, not
//	    pinned.

// stdout is the writer the subcommands print to. It's a var (not os.Stdout
// directly) so tests can swap in a buffer; it defaults to os.Stdout.
var stdout io.Writer = os.Stdout

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "plan":
		if err := runPlan(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "demo":
		fs := flag.NewFlagSet("demo", flag.ContinueOnError)
		compact := fs.Bool("compact", false, "print the day as a single JSON object instead of the human-readable report")
		if err := fs.Parse(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		runDemo(*compact)
	case "habits":
		if err := runHabits(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `osrs-golden-god — behavior-layer CLI for the OSRS bot experiment

Usage:
  osrs-golden-god plan --min-hours F --max-hours F --sessions N [--seed S]
      Plan N sessions from [min, max] hours and print the run summary.
  osrs-golden-god demo [--compact]
      Print a synthetic day (planned sessions + sample ledger summary).
      With --compact, print the day as a single JSON object for scripting.
  osrs-golden-god habits [--glance-every S] [--bank-every S] [--chat-every S]
      Print a personality-actions schedule for a 1h session: stats-tab
      glances, bank visits and chat messages at the given mean intervals
      (seconds; defaults 300/900/1200). Gaussian jitter is internal.
`)
}

func runPlan(args []string) error {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	minH := fs.Float64("min-hours", 0, "minimum session length in hours")
	maxH := fs.Float64("max-hours", 0, "maximum session length in hours")
	n := fs.Int("sessions", 0, "number of sessions to plan")
	seed := fs.Int64("seed", 1, "RNG seed (deterministic output for a fixed seed)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *minH <= 0 || *maxH < *minH || *n <= 0 {
		return fmt.Errorf("need --min-hours > 0, --max-hours >= min, --sessions > 0")
	}
	cfg := core.Config{
		MinSessionHours: *minH,
		MaxSessionHours: *maxH,
		Efficiency:      0.85,
		ClickSigmaMs:    40,
	}
	planner := core.NewSessionPlanner(cfg, rand.New(rand.NewSource(*seed)))
	lengths := planner.Sessions(*n)
	res, err := core.PlanDay(cfg, lengths)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "sessions: %d\n", len(res.Sessions))
	for i, s := range res.Sessions {
		fmt.Fprintf(stdout, "  session %d: %.1fh -> ~%d actions\n", i+1, s.Hours, s.Actions)
	}
	fmt.Fprintf(stdout, "total: %.1fh, ~%d actions", res.TotalHours, res.TotalActions)
	if cfg.DailyActionBudget > 0 {
		fmt.Fprintf(stdout, " (budget left over: %d)", res.BudgetLeftOver)
	}
	fmt.Fprintln(stdout)
	return nil
}

// runHabits prints a personality-actions schedule for a one-hour session:
// the generator's full event list (tick, class, detail) at the configured
// mean intervals. Defaults are "realistic human" cadences — a stats-tab
// glance every ~5 minutes, a bank visit every ~15, a chat every ~20. The
// Gaussian jitter around each interval is internal to the generator, so the
// printed schedule is illustrative (a different run jitters differently)
// rather than a pinned fixture; the flags exist so a reader can see how the
// cadence knobs change the schedule shape.
func runHabits(args []string) error {
	fs := flag.NewFlagSet("habits", flag.ContinueOnError)
	glanceEvery := fs.Float64("glance-every", 300, "mean seconds between stats-tab glances")
	bankEvery := fs.Float64("bank-every", 900, "mean seconds between bank visits")
	chatEvery := fs.Float64("chat-every", 1200, "mean seconds between chat messages")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := core.PersonalityConfig{
		GlanceEvery: *glanceEvery, GlanceSigma: 0.2 * *glanceEvery,
		BankEvery: *bankEvery, BankSigma: 0.15 * *bankEvery,
		ChatEvery: *chatEvery, ChatSigma: 0.25 * *chatEvery,
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	gen := core.NewPersonalityGenerator(cfg, rand.New(rand.NewSource(1)), 1.0)
	sched := gen.Schedule()

	fmt.Fprintln(stdout, "== personality-actions schedule (1h session) ==")
	fmt.Fprintf(stdout, "intervals: glance ~%gs, bank ~%gs, chat ~%gs (Gaussian jitter internal)\n",
		cfg.GlanceEvery, cfg.BankEvery, cfg.ChatEvery)
	for _, ev := range sched {
		fmt.Fprintf(stdout, "  t=%5ds  %-6s %s\n", ev.Tick/10, ev.Kind, ev.Detail)
	}
	fmt.Fprintf(stdout, "total: %d scheduled personality events in 3600s\n", len(sched))
	return nil
}

// runDemo prints a synthetic day: three planned sessions plus a sample
// ledger summary, so a fresh clone has something visible without a client.
// When compact is true it instead prints the day as a single JSON object —
// same data, shaped for scripts (jq, dashboards, tests) rather than humans.
func runDemo(compact bool) {
	cfg := core.Config{
		World:             251,
		MinSessionHours:   2,
		MaxSessionHours:   6,
		Efficiency:        0.85,
		ClickSigmaMs:      40,
		DailyActionBudget: 300,
	}
	planner := core.NewSessionPlanner(cfg, rand.New(rand.NewSource(2026)))
	lengths := []float64{}
	for i := 0; i < 3; i++ {
		lengths = append(lengths, planner.Next())
	}
	res, err := core.PlanDay(cfg, lengths)
	if err != nil {
		fmt.Fprintln(os.Stderr, "demo plan error:", err)
		os.Exit(1)
	}

	if compact {
		type sessionOut struct {
			Index   int     `json:"index"`
			Hours   float64 `json:"hours"`
			Actions int     `json:"actions"`
		}
		out := struct {
			World          int          `json:"world"`
			Sessions       []sessionOut `json:"sessions"`
			TotalHours     float64      `json:"total_hours"`
			TotalActions   int          `json:"total_actions"`
			DailyBudget    int          `json:"daily_budget"`
			BudgetLeftOver int          `json:"budget_left_over"`
		}{
			World:        cfg.World,
			Sessions:     make([]sessionOut, 0, len(res.Sessions)),
			TotalHours:   res.TotalHours,
			TotalActions: res.TotalActions,
			DailyBudget:  cfg.DailyActionBudget,
		}
		for i, s := range res.Sessions {
			out.Sessions = append(out.Sessions, sessionOut{Index: i + 1, Hours: s.Hours, Actions: s.Actions})
		}
		out.BudgetLeftOver = res.BudgetLeftOver
		b, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "demo json error:", err)
			os.Exit(1)
		}
		fmt.Fprintln(stdout, string(b))
		return
	}

	fmt.Fprintln(stdout, "== synthetic day ==")
	fmt.Fprintf(stdout, "world %d, sessions planned:\n", cfg.World)
	for i, s := range res.Sessions {
		fmt.Fprintf(stdout, "  session %d: %.1fh -> ~%d actions\n", i+1, s.Hours, s.Actions)
	}
	fmt.Fprintf(stdout, "total: %.1fh, ~%d actions (daily budget %d, left over: %d)\n",
		res.TotalHours, res.TotalActions, cfg.DailyActionBudget, res.BudgetLeftOver)
	if res.BudgetLeftOver == 0 {
		fmt.Fprintln(stdout, "(later sessions show fewer planned actions because the daily budget ran out — expected)")
	}

	// Sample ledger summary: a short slice of what a session's event log looks
	// like. Ticks are 600ms each; the span below is ~90 seconds of activity.
	ledger := &core.Ledger{}
	tick := 0
	for _, kind := range []string{"session-start", "click", "keypress", "bank", "chat", "click"} {
		if err := ledger.Append(core.Entry{Tick: tick, Kind: kind, Detail: "demo"}); err != nil {
			fmt.Fprintln(os.Stderr, "demo ledger error:", err)
			os.Exit(1)
		}
		tick += 60 // one second apart
	}
	st := ledger.Summarize()
	fmt.Fprintln(stdout, "== sample ledger summary (first ~90s of a session) ==")
	fmt.Fprintf(stdout, "sessions: %d, clicks: %d, keypresses: %d, banks: %d, chats: %d, span: %ds\n",
		st.Sessions, st.Clicks, st.Keypresses, st.Banks, st.Chats, st.DurationTicks/10)
}
