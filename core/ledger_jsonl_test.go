package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSampleLedger builds a JSONLLedger at path and appends the given
// entries, returning the ledger so the caller can Close it.
func writeSampleLedger(t *testing.T, path string, entries []Entry) *JSONLLedger {
	t.Helper()
	jl, err := OpenJSONL(path, -1)
	if err != nil {
		t.Fatalf("OpenJSONL: %v", err)
	}
	for _, e := range entries {
		if err := jl.Append(e); err != nil {
			t.Fatalf("append tick=%d: %v", e.Tick, err)
		}
	}
	return jl
}

var sampleEntries = []Entry{
	{Tick: 0, Kind: "session-start", Detail: "login world 251"},
	{Tick: 42, Kind: "click", Detail: "clicked Gnome at 5123"},
	{Tick: 95, Kind: "keypress", Detail: "pressed R for run"},
	{Tick: 120, Kind: "bank", Detail: "banked 14 gnomes"},
	{Tick: 150, Kind: "chat", Detail: "'ty'"},
	{Tick: 200, Kind: "session-end", Detail: ""},
}

func TestJSONLAppendWritesOneLinePerEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bot-20260930.jsonl")
	jl := writeSampleLedger(t, path, sampleEntries)
	if err := jl.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != len(sampleEntries) {
		t.Fatalf("file has %d lines, want %d:\n%s", len(lines), len(sampleEntries), data)
	}
	// First line must be exactly the compact form of the first entry.
	want := `{"tick":0,"kind":"session-start","detail":"login world 251"}`
	if lines[0] != want {
		t.Errorf("line 1 = %s, want %s", lines[0], want)
	}
}

func TestJSONLRoundTripThroughFile(t *testing.T) {
	path := LedgerPath(t.TempDir(), "golden-god", "20260930")
	jl := writeSampleLedger(t, path, sampleEntries)
	if err := jl.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	back, lastTick, err := ReadJSONL(path)
	if err != nil {
		t.Fatalf("ReadJSONL: %v", err)
	}
	if lastTick != 200 {
		t.Errorf("lastTick = %d, want 200", lastTick)
	}
	if len(back) != len(sampleEntries) {
		t.Fatalf("read back %d entries, want %d", len(back), len(sampleEntries))
	}
	for i := range sampleEntries {
		if back[i] != sampleEntries[i] {
			t.Errorf("entry %d = %+v, want %+v", i, back[i], sampleEntries[i])
		}
	}
	// The summary computed from the read-back ledger must match one computed
	// directly — the round-trip preserved everything Summarize cares about.
	want := (&Ledger{Entries: sampleEntries}).Summarize()
	got := (&Ledger{Entries: back}).Summarize()
	if got != want {
		t.Errorf("summary mismatch after round trip:\n got %+v\nwant %+v", got, want)
	}
}

func TestJSONLAppendEnforcesMonotonicAgainstFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	jl := writeSampleLedger(t, path, sampleEntries)
	if err := jl.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// A fresh process continuing the same file, seeded with the last tick it
	// read back: an older tick must be rejected before it touches disk.
	_, lastTick, err := ReadJSONL(path)
	if err != nil {
		t.Fatalf("read for seed: %v", err)
	}
	next, err := OpenJSONL(path, lastTick)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if err := next.Append(Entry{Tick: 10, Kind: "click", Detail: "late click"}); !errors.Is(err, ErrDuplicateTick) {
		t.Errorf("stale tick: got %v, want ErrDuplicateTick", err)
	}
	// The file must be unchanged (still exactly the original lines).
	data, _ := os.ReadFile(path)
	if got := strings.TrimRight(string(data), "\n"); strings.Count(got, "\n") != len(sampleEntries)-1 {
		t.Errorf("file gained lines from a rejected append:\n%s", data)
	}
	// ...but a strictly-later tick appends fine.
	if err := next.Append(Entry{Tick: 300, Kind: "click", Detail: "after restart"}); err != nil {
		t.Errorf("later tick after reopen: %v", err)
	}
	if err := next.Close(); err != nil { // explicit flush before re-reading the file
		t.Fatalf("close after append: %v", err)
	}
	back, _, err := ReadJSONL(path)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if len(back) != len(sampleEntries)+1 || back[len(back)-1].Tick != 300 {
		t.Errorf("after append+read: got %d entries, last tick %+v", len(back), back[len(back)-1])
	}
}

func TestReadJSONLRejectsMalformedLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.jsonl")
	if err := os.WriteFile(path, []byte(`{"tick":1,"kind":"click","detail":"ok"}`+"\n"+"not json at all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := ReadJSONL(path)
	if !errors.Is(err, ErrMalformedLedgerLine) {
		t.Errorf("got %v, want ErrMalformedLedgerLine", err)
	}
}

func TestReadJSONLRejectsNonIncreasingTick(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.jsonl")
	if err := os.WriteFile(path, []byte(
		`{"tick":5,"kind":"click","detail":"a"}`+"\n"+
			`{"tick":5,"kind":"click","detail":"b"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := ReadJSONL(path)
	if !errors.Is(err, ErrNonIncreasingTick) {
		t.Errorf("got %v, want ErrNonIncreasingTick", err)
	}
}

func TestReadJSONLEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	entries, lastTick, err := ReadJSONL(path)
	if err != nil {
		t.Fatalf("ReadJSONL on empty file: %v", err)
	}
	if len(entries) != 0 || lastTick != -1 {
		t.Errorf("empty file = (%d entries, lastTick %d), want (0, -1)", len(entries), lastTick)
	}
}

func TestReadJSONLMissingFile(t *testing.T) {
	_, _, err := ReadJSONL(filepath.Join(t.TempDir(), "nope.jsonl"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("got %v, want ErrNotExist", err)
	}
}

func TestLedgerPath(t *testing.T) {
	got := LedgerPath("/data/ledgers", "golden-god", "20260930")
	want := filepath.Join("/data/ledgers", "golden-god-20260930.jsonl")
	if got != want {
		t.Errorf("LedgerPath = %q, want %q", got, want)
	}
}
