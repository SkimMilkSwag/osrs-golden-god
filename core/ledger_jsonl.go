package core

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// JSONLLedger is a Ledger that also persists each entry to a .jsonl file, one
// line per event, as it's appended. That's the "real deployment" half of the
// ledger design: the in-memory Ledger stays the fast path (tick-monotonic
// checks, Summarize), and the file is the durable, post-hoc ban-appeal
// evidence that survives a client crash mid-session.
type JSONLLedger struct {
	*Ledger

	w *bufio.Writer
	f *os.File
}

// OpenJSONL opens (creating or appending to) path and returns a JSONLLedger
// that writes every Append as one compact JSON line: {"tick":N,"kind":"...","detail":"..."}.
// Appending to an existing file is expected — one file per day, sessions
// append in order. The lastTick (if >= 0) seeds the tick-monotonic check from
// whatever the previous run left in the file, so a fresh process continuing
// the same day can't log backwards into it.
func OpenJSONL(path string, lastTick int) (*JSONLLedger, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open ledger file: %w", err)
	}
	l := &Ledger{}
	if lastTick >= 0 {
		// Sentinel entry so Append enforces monotonicity against the file's
		// existing content without re-reading it. It is never written out.
		l.Entries = append(l.Entries, Entry{Tick: lastTick})
	}
	return &JSONLLedger{Ledger: l, w: bufio.NewWriter(f), f: f}, nil
}

// Append records the entry and flushes it as a single JSON line. The tick
// check runs first, so a rejected entry never reaches the file.
func (j *JSONLLedger) Append(e Entry) error {
	if err := j.Ledger.Append(e); err != nil {
		return err
	}
	b, err := json.Marshal(e)
	if err != nil {
		// Only Tick/Kind/Detail (all always-encodable types), so this can't
		// actually fail; keep the error path anyway.
		return fmt.Errorf("marshal ledger entry: %w", err)
	}
	b = append(b, '\n')
	if _, err := j.w.Write(b); err != nil {
		return fmt.Errorf("write ledger line: %w", err)
	}
	return nil
}

// Close flushes the buffered lines and closes the underlying file. Callers
// should handle a non-nil error (a half-flushed file is worth knowing about).
func (j *JSONLLedger) Close() error {
	if err := j.w.Flush(); err != nil {
		return fmt.Errorf("flush ledger: %w", err)
	}
	return j.f.Close()
}

var (
	// ErrMalformedLedgerLine is returned by ReadJSONL for a non-empty line
	// that is not a valid Entry JSON object.
	ErrMalformedLedgerLine = errors.New("malformed ledger line")
	// ErrNonIncreasingTick is returned by ReadJSONL when the file itself
	// violates tick monotonicity (corruption or a mid-write crash).
	ErrNonIncreasingTick = errors.New("non-increasing tick in ledger file")
)

// ReadJSONL reads a .jsonl ledger back into memory, validating two things a
// blind trust would miss: every line parses as an Entry, and ticks are
// strictly increasing across the whole file (the Append invariant, checked
// now on disk). It returns the parsed entries plus the last tick in the file
// (-1 for an empty file) so a caller can seed OpenJSONL with it.
func ReadJSONL(path string) ([]Entry, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, -1, err
	}
	defer f.Close()

	var entries []Entry
	lastTick := -1
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Bytes()
		if len(line) == 0 {
			continue // tolerate a trailing blank line
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, lastTick, fmt.Errorf("%w at line %d: %v", ErrMalformedLedgerLine, lineNo, err)
		}
		if e.Tick <= lastTick {
			return nil, lastTick, fmt.Errorf("%w at line %d: tick %d after %d", ErrNonIncreasingTick, lineNo, e.Tick, lastTick)
		}
		entries = append(entries, e)
		lastTick = e.Tick
	}
	if err := sc.Err(); err != nil {
		return nil, lastTick, fmt.Errorf("read ledger file: %w", err)
	}
	return entries, lastTick, nil
}

// LedgerPath is a convenience for the conventional location of a day's ledger
// file under dir (one per bot): <dir>/<bot>-<yyyymmdd>.jsonl.
func LedgerPath(dir, bot string, yyyymmdd string) string {
	return filepath.Join(dir, fmt.Sprintf("%s-%s.jsonl", bot, yyyymmdd))
}
