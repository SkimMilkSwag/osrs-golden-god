package core

import (
	"errors"
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
