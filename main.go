package main

import (
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
)

// main is the thin CLI wrapper over the behavior layer. The two subcommands
// today:
//
//	osrs-golden-god plan --min-hours 2 --max-hours 6 --sessions 3 [--seed N]
//	    Plans N sessions from the given range and prints a RunResult summary.
//	    Deterministic for a fixed --seed (defaults to 1).
//
//	osrs-golden-god demo
//	    Prints a synthetic day: three planned sessions plus a sample ledger
//	    summary, so a fresh clone has something visible without a client.

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
		runDemo()
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
  osrs-golden-god demo
      Print a synthetic day (planned sessions + sample ledger summary).
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
	cfg := Config{
		MinSessionHours: *minH,
		MaxSessionHours: *maxH,
		Efficiency:      0.85,
		ClickSigmaMs:    40,
	}
	planner := NewSessionPlanner(cfg, rand.New(rand.NewSource(*seed)))
	lengths := make([]float64, 0, *n)
	for i := 0; i < *n; i++ {
		lengths = append(lengths, planner.Next())
	}
	res, err := PlanDay(cfg, lengths)
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

func runDemo() {
	cfg := Config{
		World:             251,
		MinSessionHours:   2,
		MaxSessionHours:   6,
		Efficiency:        0.85,
		ClickSigmaMs:      40,
		DailyActionBudget: 300,
	}
	planner := NewSessionPlanner(cfg, rand.New(rand.NewSource(2026)))
	lengths := []float64{}
	for i := 0; i < 3; i++ {
		lengths = append(lengths, planner.Next())
	}
	res, err := PlanDay(cfg, lengths)
	if err != nil {
		fmt.Fprintln(os.Stderr, "demo plan error:", err)
		os.Exit(1)
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
	ledger := &Ledger{}
	tick := 0
	for _, kind := range []string{"session-start", "click", "keypress", "bank", "chat", "click"} {
		if err := ledger.Append(Entry{Tick: tick, Kind: kind, Detail: "demo"}); err != nil {
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
