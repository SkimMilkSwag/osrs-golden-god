package main

import (
	"bytes"
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
	out := captureStdout(t, runDemo)
	for _, want := range []string{"synthetic day", "sample ledger summary"} {
		if !strings.Contains(out, want) {
			t.Errorf("demo output missing %q:\n%s", want, out)
		}
	}
}
