package main

import (
	"errors"
	"fmt"
)

// Ledger is a minimal, append-only record of what a bot session did. A real
// deployment writes one line per entry to a JSONL file; the structure here is
// what that file's rows look like. Keeping it tiny on purpose: the ledger is
// for post-hoc ban-appeal evidence and for tuning the anti-ban knobs, not
// for replaying the whole day.
type Ledger struct {
	// Entries are appended in order; the slice is never shrunk.
	Entries []Entry
}

// Entry is one logged event from a session.
type Entry struct {
	// Tick is the OSRS game tick (600ms each) when the event was recorded.
	Tick int
	// Kind is a short tag for the event class, e.g. "click", "keypress",
	// "bank", "chat", "session-start", "session-end".
	Kind string
	// Detail is free-form, human-readable context (e.g. "clicked Gnome at 5123").
	Detail string
}

var (
	// ErrDuplicateTick is returned when two entries carry the same tick — a
	// sign of a double-logged reflex that should be investigated.
	ErrDuplicateTick = errors.New("duplicate tick in ledger")
)

// Append records an event. Ticks must be strictly increasing: the game clock
// doesn't go backwards, so a repeat or regression means a logging bug.
func (l *Ledger) Append(e Entry) error {
	if len(l.Entries) > 0 && e.Tick <= l.Entries[len(l.Entries)-1].Tick {
		return fmt.Errorf("%w: %d", ErrDuplicateTick, e.Tick)
	}
	l.Entries = append(l.Entries, e)
	return nil
}

// Stats summarizes the ledger for the daily report.
type Stats struct {
	Sessions      int
	Clicks        int
	Keypresses    int
	Banks         int
	Chats         int
	DurationTicks int // span from first to last entry, inclusive
}

// Summarize counts entries by kind and computes the session time span.
// It does not validate ticks (Append already enforces that).
func (l *Ledger) Summarize() Stats {
	var s Stats
	for _, e := range l.Entries {
		switch e.Kind {
		case "click":
			s.Clicks++
		case "keypress":
			s.Keypresses++
		case "bank":
			s.Banks++
		case "chat":
			s.Chats++
		case "session-start":
			s.Sessions++
		}
	}
	if len(l.Entries) > 0 {
		first := l.Entries[0].Tick
		last := l.Entries[len(l.Entries)-1].Tick
		s.DurationTicks = last - first
	}
	return s
}
