package core

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestLedgerAppendEnforcesIncreasingTicks(t *testing.T) {
	l := &Ledger{}
	if err := l.Append(Entry{Tick: 10, Kind: "click", Detail: "first"}); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if err := l.Append(Entry{Tick: 20, Kind: "keypress", Detail: "second"}); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	if err := l.Append(Entry{Tick: 20, Kind: "click", Detail: "same tick"}); !errors.Is(err, ErrDuplicateTick) {
		t.Errorf("same tick: got %v, want ErrDuplicateTick", err)
	}
	if err := l.Append(Entry{Tick: 5, Kind: "click", Detail: "earlier tick"}); !errors.Is(err, ErrDuplicateTick) {
		t.Errorf("decreasing tick: got %v, want ErrDuplicateTick", err)
	}
	if len(l.Entries) != 2 {
		t.Errorf("entries = %d, want 2 (failed appends should not append)", len(l.Entries))
	}
}

func TestLedgerSummarizeCountsByKind(t *testing.T) {
	l := &Ledger{}
	events := []Entry{
		{Tick: 0, Kind: "session-start"},
		{Tick: 10, Kind: "click"},
		{Tick: 20, Kind: "click"},
		{Tick: 30, Kind: "keypress"},
		{Tick: 40, Kind: "bank"},
		{Tick: 50, Kind: "chat"},
		{Tick: 60, Kind: "glance"},
		{Tick: 70, Kind: "click"},
	}
	for i := range events {
		if err := l.Append(events[i]); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	st := l.Summarize()
	if st.Sessions != 1 {
		t.Errorf("Sessions = %d, want 1", st.Sessions)
	}
	if st.Clicks != 3 {
		t.Errorf("Clicks = %d, want 3", st.Clicks)
	}
	if st.Keypresses != 1 {
		t.Errorf("Keypresses = %d, want 1", st.Keypresses)
	}
	if st.Banks != 1 {
		t.Errorf("Banks = %d, want 1", st.Banks)
	}
	if st.Chats != 1 {
		t.Errorf("Chats = %d, want 1", st.Chats)
	}
	if st.Glances != 1 {
		t.Errorf("Glances = %d, want 1", st.Glances)
	}
	if st.DurationTicks != 70 {
		t.Errorf("DurationTicks = %d, want 70 (tick 0 to tick 70)", st.DurationTicks)
	}
}

func TestLedgerSummarizeEmpty(t *testing.T) {
	st := (&Ledger{}).Summarize()
	if st.DurationTicks != 0 || st.Sessions != 0 {
		t.Errorf("empty ledger summary = %+v, want all zeros", st)
	}
}

// sampleLedger returns a ledger whose entries cover every known kind plus one
// unknown kind ("death"), in a fixed tick order. Both tests below build on it
// so they exercise the exact same data.
func sampleLedger(t *testing.T) *Ledger {
	t.Helper()
	l := &Ledger{}
	events := []Entry{
		{Tick: 0, Kind: "session-start"},
		{Tick: 10, Kind: "click"},
		{Tick: 20, Kind: "click"},
		{Tick: 30, Kind: "keypress"},
		{Tick: 40, Kind: "bank"},
		{Tick: 50, Kind: "chat"},
		{Tick: 60, Kind: "glance"},
		{Tick: 70, Kind: "death"}, // unknown kind: not in Stats' named fields
		{Tick: 80, Kind: "click"},
		{Tick: 90, Kind: "session-end"},
	}
	for i := range events {
		if err := l.Append(events[i]); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	return l
}

func TestLedgerKindCountsIncludesUnknownKinds(t *testing.T) {
	l := sampleLedger(t)
	counts, span := l.KindCounts()
	want := map[string]int{
		"session-start": 1,
		"click":         3,
		"keypress":      1,
		"bank":          1,
		"chat":          1,
		"glance":        1,
		"death":         1, // an unknown kind must still be counted, not dropped
		"session-end":   1,
	}
	if len(counts) != len(want) {
		t.Fatalf("KindCounts returned %d kinds (%v), want %d (%v)", len(counts), counts, len(want), want)
	}
	for k, n := range want {
		if counts[k] != n {
			t.Errorf("KindCounts[%q] = %d, want %d", k, counts[k], n)
		}
	}
	if span != 90 {
		t.Errorf("span = %d, want 90 (tick 0 to tick 90)", span)
	}

	// Summarize must be a faithful projection of the same tally: its named
	// fields match the per-kind counts, and the unknown kind simply has no
	// named field (that's what KindCounts is for).
	st := l.Summarize()
	if st.Clicks != counts["click"] || st.Keypresses != counts["keypress"] ||
		st.Banks != counts["bank"] || st.Chats != counts["chat"] ||
		st.Glances != counts["glance"] || st.Sessions != counts["session-start"] {
		t.Errorf("Summarize diverged from KindCounts: %+v vs %v", st, counts)
	}
	if st.DurationTicks != span {
		t.Errorf("DurationTicks = %d, KindCounts span = %d — must agree", st.DurationTicks, span)
	}

	// Empty ledger: empty map, zero span.
	counts, span = (&Ledger{}).KindCounts()
	if len(counts) != 0 || span != 0 {
		t.Errorf("empty KindCounts = (%v, %d), want (map[], 0)", counts, span)
	}
}

func TestLedgerBuildReportRowsAndTotals(t *testing.T) {
	l := sampleLedger(t)
	rep := l.BuildReport()

	wantRows := []string{
		"session-start: 1", // first-seen order: the ledger's own chronology
		"click: 3",
		"keypress: 1",
		"bank: 1",
		"chat: 1",
		"glance: 1",
		"death: 1", // unknown kind gets a row too — that's the point
		"session-end: 1",
	}
	if len(rep.Rows) != len(wantRows) {
		t.Fatalf("report rows = %d (%v), want %d", len(rep.Rows), rep.Rows, len(wantRows))
	}
	for i := range wantRows {
		if rep.Rows[i] != wantRows[i] {
			t.Errorf("row %d = %q, want %q", i, rep.Rows[i], wantRows[i])
		}
	}

	var total int
	for _, row := range rep.Rows {
		var n int
		if _, err := fmt.Sscanf(row[strings.LastIndex(row, ":")+1:], "%d", &n); err != nil {
			t.Fatalf("row %q has no parseable count: %v", row, err)
		}
		total += n
	}
	if rep.Total != 10 || total != rep.Total {
		t.Errorf("Total = %d (rows sum %d), want 10 and equal to the row sum", rep.Total, total)
	}
	if rep.Seconds != 9 {
		t.Errorf("Seconds = %d, want 9 (tick 0..90 at 600ms each)", rep.Seconds)
	}

	// Empty ledger: no rows, zero totals — a quiet day stays quiet.
	rep = (&Ledger{}).BuildReport()
	if len(rep.Rows) != 0 || rep.Total != 0 || rep.Seconds != 0 {
		t.Errorf("empty report = %+v, want all empty/zero", rep)
	}
}
