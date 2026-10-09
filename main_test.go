package main

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"
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
