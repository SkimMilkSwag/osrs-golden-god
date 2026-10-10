package core

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

// Entry is one logged event from a session. The json tags pin the on-disk
// wire format for the JSONL file — the ledger's rows are shared evidence, so
// the field names there must not drift.
type Entry struct {
	// Tick is the OSRS game tick (600ms each) when the event was recorded.
	Tick int `json:"tick"`
	// Kind is a short tag for the event class, e.g. "click", "keypress",
	// "bank", "chat", "session-start", "session-end".
	Kind string `json:"kind"`
	// Detail is free-form, human-readable context (e.g. "clicked Gnome at 5123").
	Detail string `json:"detail"`
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

// Stats summarizes the ledger for the daily report. Its named fields are a
// projection over KindCounts (one tally, two views), so they can't drift
// out of sync with the per-kind counts.
type Stats struct {
	Sessions      int
	Clicks        int
	Keypresses    int
	Banks         int
	Chats         int
	Glances       int // stats-tab glances (personality events)
	DurationTicks int // span from first to last entry, inclusive
}

// KindCounts tallies ledger entries by kind and computes the session time
// span. Unknown kinds count too: a new event class (or a typo in one) stays
// visible in the report instead of silently vanishing from the named fields.
// It does not validate ticks (Append already enforces that).
func (l *Ledger) KindCounts() (map[string]int, int) {
	counts := map[string]int{}
	for _, e := range l.Entries {
		counts[e.Kind]++
	}
	var span int
	if n := len(l.Entries); n > 0 {
		span = l.Entries[n-1].Tick - l.Entries[0].Tick
	}
	return counts, span
}

// Summarize counts entries by kind and computes the session time span. It is
// a thin projection over KindCounts: the named fields stay for existing
// callers, and both views come from one tally so they can't disagree.
func (l *Ledger) Summarize() Stats {
	counts, span := l.KindCounts()
	return Stats{
		Sessions:      counts["session-start"],
		Clicks:        counts["click"],
		Keypresses:    counts["keypress"],
		Banks:         counts["bank"],
		Chats:         counts["chat"],
		Glances:       counts["glance"],
		DurationTicks: span,
	}
}

// Report is the daily-report view of a ledger: one row per event kind, in
// first-seen order (the ledger's own chronology), plus total entries and the
// session time span in seconds. Kinds nobody logged produce no rows, so a
// quiet day stays quiet; a kind nobody *knew* about still shows up — that's
// how you find out a new event class landed in a file.
type Report struct {
	// Rows is one line of the report per kind, e.g. "click: 1342".
	Rows []string
	// Total entries in the ledger (sum over all kinds).
	Total int
	// Seconds from the first to the last entry (0 for an empty ledger).
	Seconds int

	counts map[string]int // per-kind tally kept for Counts()
}

// Counts is the per-kind tally behind a Report — the machine-readable form of
// the same data Rows renders, derived from the ledger's entries rather than
// by parsing formatted strings.
func (r *Report) Counts() map[string]int {
	return r.counts
}

// BuildReport renders the ledger into a daily report. The order of Rows is
// deterministic — first-seen, per tick order — so a fixed day's file always
// prints the same report, and tests can pin full outputs. The per-kind map it
// tallies along the way is kept for Report.Counts().
func (l *Ledger) BuildReport() Report {
	counts := map[string]int{}
	var order []string
	total := 0
	for _, e := range l.Entries {
		if _, seen := counts[e.Kind]; !seen {
			order = append(order, e.Kind)
		}
		counts[e.Kind]++
		total++
	}
	var seconds int
	if n := len(l.Entries); n > 0 {
		seconds = (l.Entries[n-1].Tick - l.Entries[0].Tick) / 10 // 600ms ticks -> seconds
	}
	rep := Report{Total: total, Seconds: seconds, counts: counts}
	for _, kind := range order {
		rep.Rows = append(rep.Rows, fmt.Sprintf("%s: %d", kind, counts[kind]))
	}
	return rep
}
