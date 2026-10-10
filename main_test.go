package main

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SkimMilkSwag/osrs-golden-god/core"
)

// captureStdout runs fn and returns whatever it printed to the package-level
// stdout writer (a var so tests can swap in a buffer without touching os).
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	old := stdout
	stdout = &buf
	defer func() { stdout = old }()
	fn()
	return buf.String()
}

func TestPlanSubcommandOutput(t *testing.T) {
	out := captureStdout(t, func() {
		if err := runPlan([]string{"--min-hours", "2", "--max-hours", "6", "--sessions", "3", "--seed", "7"}); err != nil {
			t.Fatalf("runPlan: %v", err)
		}
	})
	if !strings.Contains(out, "sessions: 3") {
		t.Errorf("output missing session count header:\n%s", out)
	}
	if !strings.Contains(out, "total:") {
		t.Errorf("output missing totals line:\n%s", out)
	}
}

func TestPlanSubcommandDeterministicForSeed(t *testing.T) {
	run := func() string {
		return captureStdout(t, func() {
			if err := runPlan([]string{"--min-hours", "2", "--max-hours", "6", "--sessions", "5", "--seed", "99"}); err != nil {
				t.Fatalf("runPlan: %v", err)
			}
		})
	}
	if a, b := run(), run(); a != b {
		t.Errorf("same seed produced different output:\n--- a ---\n%s\n--- b ---\n%s", a, b)
	}
}

func TestPlanSubcommandRejectsBadFlags(t *testing.T) {
	out := captureStdout(t, func() {
		err := runPlan([]string{"--min-hours", "5", "--max-hours", "2", "--sessions", "1"})
		if err == nil {
			t.Fatal("expected error for min > max")
		}
	})
	_ = out
}

func TestDemoSubcommandRuns(t *testing.T) {
	out := captureStdout(t, func() { runDemo(false) })
	for _, want := range []string{"synthetic day", "sample ledger summary"} {
		if !strings.Contains(out, want) {
			t.Errorf("demo output missing %q:\n%s", want, out)
		}
	}
}

func TestDemoSubcommandCompactIsJSON(t *testing.T) {
	out := captureStdout(t, func() { runDemo(true) })

	var doc struct {
		World    int `json:"world"`
		Sessions []struct {
			Index   int     `json:"index"`
			Hours   float64 `json:"hours"`
			Actions int     `json:"actions"`
		} `json:"sessions"`
		TotalHours   float64 `json:"total_hours"`
		TotalActions int     `json:"total_actions"`
		DailyBudget  int     `json:"daily_budget"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("compact demo output is not a JSON object: %v\n%s", err, out)
	}
	if doc.World != 251 || len(doc.Sessions) != 3 {
		t.Errorf("world=%d sessions=%d, want world 251 and 3 sessions", doc.World, len(doc.Sessions))
	}
	var totalHours float64
	totalActions := 0
	for i, s := range doc.Sessions {
		if s.Index != i+1 || s.Hours < 2 || s.Hours > 6 {
			t.Errorf("session %d out of range: %+v", i, s)
		}
		if s.Actions < 0 || s.Actions > doc.DailyBudget {
			t.Errorf("session %d actions %d outside [0, budget]: %+v", i, s.Actions, s)
		}
		totalHours += s.Hours
		totalActions += s.Actions
	}
	if math.Abs(totalHours-doc.TotalHours) > 1e-9 || totalActions != doc.TotalActions {
		t.Errorf("totals inconsistent: sum(%g, %d) vs reported (%g, %d)",
			totalHours, totalActions, doc.TotalHours, doc.TotalActions)
	}
	if doc.DailyBudget == 300 && totalActions > 300 {
		t.Errorf("daily budget exceeded: %d actions planned for a 300-action budget", totalActions)
	}
	if doc.DailyBudget != 300 {
		t.Errorf("daily_budget = %d, want 300", doc.DailyBudget)
	}
	if strings.Contains(out, "synthetic day") || strings.Contains(out, "==") {
		t.Errorf("compact mode leaked the human-readable report:\n%s", out)
	}
}

func TestDemoCompactDeterministic(t *testing.T) {
	run := func() string {
		return captureStdout(t, func() { runDemo(true) })
	}
	if a, b := run(), run(); a != b {
		t.Errorf("same demo produced different compact output:\n%s\n---\n%s", a, b)
	}
}

func TestHabitsSubcommandOutput(t *testing.T) {
	out := captureStdout(t, func() {
		if err := runHabits([]string{}); err != nil {
			t.Fatalf("runHabits: %v", err)
		}
	})
	for _, want := range []string{
		"personality-actions schedule (1h session)",
		"intervals: glance ~300s, bank ~900s, chat ~1200s",
		"stats-tab glance", "bank visit", "chat 'ty'",
		"total:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("habits output missing %q:\n%s", want, out)
		}
	}
	// Every scheduled event must fit inside the hour.
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "t=") {
			continue
		}
		numStr := strings.TrimPrefix(trimmed, "t=")
		var s int
		for _, c := range numStr {
			if c < '0' || c > '9' {
				break
			}
			s = s*10 + int(c-'0')
		}
		if s > 3600 {
			t.Errorf("event at t=%ds is past the 1h session end", s)
		}
	}
}

func TestHabitsSubcommandRejectsBadIntervals(t *testing.T) {
	captureStdout(t, func() {
		if err := runHabits([]string{"--glance-every", "0"}); err == nil {
			t.Fatal("expected error for zero glance interval")
		}
	})
}

func TestHabitsSubcommandDeterministicForDefaults(t *testing.T) {
	run := func() string {
		return captureStdout(t, func() {
			if err := runHabits([]string{}); err != nil {
				t.Fatalf("runHabits: %v", err)
			}
		})
	}
	if a, b := run(), run(); a != b {
		t.Errorf("same defaults produced different schedules:\n--- a ---\n%s\n--- b ---\n%s", a, b)
	}
}

func TestHabitsSubcommandTighterIntervalsScheduleMore(t *testing.T) {
	count := func(args []string) int {
		out := captureStdout(t, func() {
			if err := runHabits(args); err != nil {
				t.Fatalf("runHabits: %v", err)
			}
		})
		var n int
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "t=") {
				n++
			}
		}
		return n
	}
	if n := count([]string{"--glance-every", "30", "--bank-every", "40", "--chat-every", "50"}); n < 30 {
		t.Errorf("tight intervals scheduled only %d events in an hour, want at least ~36 (3600/100 * 3 classes)", n)
	}
}

// reportLedgerFile writes a small but realistic day's ledger to a .jsonl file
// (via the real JSONLLedger path, so the wire format is what ReadJSONL sees):
// one session with clicks, a keypress, a bank visit, a chat and a stats-tab
// glance, plus the session-end marker.
func reportLedgerFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bot-20261010.jsonl")
	jl, err := core.OpenJSONL(path, -1)
	if err != nil {
		t.Fatalf("OpenJSONL: %v", err)
	}
	events := []core.Entry{
		{Tick: 0, Kind: "session-start", Detail: "4.0h, ~24 actions"},
		{Tick: 10, Kind: "click", Detail: "click #1"},
		{Tick: 20, Kind: "click", Detail: "click #2"},
		{Tick: 30, Kind: "keypress", Detail: "keypress #3"},
		{Tick: 40, Kind: "bank", Detail: "bank visit at t=4s"},
		{Tick: 50, Kind: "chat", Detail: "chat 'ty' at t=5s"},
		{Tick: 60, Kind: "glance", Detail: "stats-tab glance at t=6s"},
		{Tick: 70, Kind: "click", Detail: "click #4"},
		{Tick: 80, Kind: "session-end", Detail: "4.0h"},
	}
	for _, e := range events {
		if err := jl.Append(e); err != nil {
			t.Fatalf("append %+v: %v", e, err)
		}
	}
	if err := jl.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return path
}

func TestReportSubcommandPrintsPerKindRows(t *testing.T) {
	path := reportLedgerFile(t)
	out := captureStdout(t, func() {
		if err := runReport(path); err != nil {
			t.Fatalf("runReport: %v", err)
		}
	})

	// One row per kind, in first-seen (chronological) order.
	for _, want := range []string{
		"ledger ",
		"session-start: 1",
		"click: 3",
		"keypress: 1",
		"bank: 1",
		"chat: 1",
		"glance: 1",
		"session-end: 1",
		"total: 9 entries, span: 8s",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report output missing %q:\n%s", want, out)
		}
	}
	// Row order is the ledger's own chronology: session-start before click.
	if i, j := strings.Index(out, "session-start: 1"), strings.Index(out, "click: 3"); i < 0 || j < 0 || i > j {
		t.Errorf("rows not in first-seen order:\n%s", out)
	}
}

func TestReportSubcommandMissingFileErrors(t *testing.T) {
	captureStdout(t, func() {
		if err := runReport(filepath.Join(t.TempDir(), "nope.jsonl")); err == nil {
			t.Fatal("report on a missing file: want error, got nil")
		}
	})
}

func TestReportSubcommandRejectsCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.jsonl")
	valid := []string{
		`{"tick":10,"kind":"click","detail":"a"}`,
		`not-json`, // line 2 is corrupt
	}
	if err := os.WriteFile(path, []byte(strings.Join(valid, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	captureStdout(t, func() {
		err := runReport(path)
		if err == nil {
			t.Fatal("corrupt file: want error, got nil")
		}
		if !strings.Contains(err.Error(), "line 2") {
			t.Errorf("error %q should name the offending line", err)
		}
	})

	// A tick regression is just as corrupt: line 1 at tick 30 after line 0's
	// tick 10.
	regressed := []string{
		`{"tick":10,"kind":"click","detail":"a"}`,
		`{"tick":30,"kind":"click","detail":"b"}`,
		`{"tick":20,"kind":"click","detail":"c"}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(regressed, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	captureStdout(t, func() {
		err := runReport(path)
		if err == nil {
			t.Fatal("non-increasing tick file: want error, got nil")
		}
		if !strings.Contains(err.Error(), "line 3") {
			t.Errorf("error %q should name the offending line", err)
		}
	})
}

func TestReportSubcommandEmptyFileIsQuiet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	out := captureStdout(t, func() {
		if err := runReport(path); err != nil {
			t.Fatalf("runReport on empty file: %v", err)
		}
	})
	if !strings.Contains(out, "total: 0 entries, span: 0s") {
		t.Errorf("empty-file report missing zero totals:\n%s", out)
	}
	// The only line besides the header is the totals line: no kind rows.
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "ledger ") {
		t.Errorf("empty-file report = %d lines (%v), want header + totals only", len(lines), lines)
	}
}
