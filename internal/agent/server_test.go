package agent

import (
	"bytes"
	"io"
	"math"
	"math/rand"
	"strings"
	"testing"

	"github.com/SkimMilkSwag/osrs-golden-god/core"
)

// kindFromLine extracts the "kind" field of a wire line without importing
// encoding/json into this file (the fake client deals in raw bytes on purpose).
func kindFromLine(line string) string {
	if i := strings.Index(line, `"kind":"`); i >= 0 {
		rest := line[i+len(`"kind":"`):]
		if j := strings.Index(rest, `"`); j >= 0 {
			return rest[:j]
		}
	}
	return ""
}

// actionLineCount returns how many wire lines are action messages.
func actionLineCount(lines []string) int {
	n := 0
	for _, l := range lines {
		if kindFromLine(l) == string(Action) {
			n++
		}
	}
	return n
}

// kindCounts tallies ledger entries by kind, so tests can assert exactly how
// many of each habit class a run produced.
func kindCounts(l *core.Ledger) map[string]int {
	m := map[string]int{}
	for _, e := range l.Entries {
		m[e.Kind]++
	}
	return m
}

// fakeClient is a scriptable wire peer: it returns the queued messages in
// order, then io.EOF. It records everything the server writes so tests can
// assert on the exact conversation that happened. The reader/writer adapters
// are structs (not func types) because go.mod targets 1.23, before
// io.ReaderFunc existed.
type fakeClient struct {
	in  []Msg
	err error    // injected once, after all queued messages are consumed
	pos int      // how many queued messages the reader has served
	got [][]byte // raw lines written by the server
}

type inReader struct{ c *fakeClient }

func (r inReader) Read(p []byte) (int, error) {
	c := r.c
	if c.pos < len(c.in) {
		m := c.in[c.pos]
		c.pos++
		buf := &bytes.Buffer{}
		if err := WriteMsg(buf, &m); err != nil {
			return 0, err
		}
		n := copy(p, buf.Bytes())
		if n < len(buf.Bytes()) {
			// The pipe broke mid-message: the bytes that fit arrived, the rest
			// are lost. The next read reports how the line actually ended (the
			// injected wire error or plain EOF).
			return n, io.ErrUnexpectedEOF
		}
		return n, nil
	}
	if c.err != nil {
		e := c.err
		c.err = nil // one-shot: subsequent reads are plain EOF
		return 0, e
	}
	return 0, io.EOF
}

type outWriter struct{ c *fakeClient }

func (w outWriter) Write(p []byte) (int, error) {
	w.c.got = append(w.c.got, append([]byte(nil), p...))
	return len(p), nil
}

// lines returns the raw messages the server wrote, one element per line.
func (c *fakeClient) lines() []string {
	out := make([]string, len(c.got))
	for i, raw := range c.got {
		out[i] = strings.TrimSuffix(string(raw), "\n")
	}
	return out
}

// serverScripted returns a Server whose ledger is fresh and whose RNG is
// seeded for determinism.
func newTestServer(seed int64) (*Server, *core.Ledger) {
	ledger := &core.Ledger{}
	return NewServer(ledger, rand.New(rand.NewSource(seed))), ledger
}

func TestFromRunPreservesPlan(t *testing.T) {
	res, err := core.PlanDay(core.Config{MinSessionHours: 1, MaxSessionHours: 8, Efficiency: 0.85}, []float64{2.0, 3.5})
	if err != nil {
		t.Fatalf("PlanDay: %v", err)
	}
	plans := FromRun(res)
	if len(plans) != len(res.Sessions) {
		t.Fatalf("len(plans) = %d, want %d", len(plans), len(res.Sessions))
	}
	for i := range plans {
		if plans[i] != (SessionPlan{Hours: res.Sessions[i].Hours, Actions: res.Sessions[i].Actions}) {
			t.Errorf("plan[%d] = %+v, want %+v", i, plans[i], res.Sessions[i])
		}
	}
	if got := FromRun(&core.RunResult{}); got != nil && len(got) != 0 {
		t.Errorf("FromRun(empty) = %+v, want empty slice", got)
	}
}

func TestRunDayPlansAndExecutes(t *testing.T) {
	cfg := core.Config{MinSessionHours: 1, MaxSessionHours: 8, Efficiency: 0.85}
	res, err := core.PlanDay(cfg, []float64{2.0})
	if err != nil {
		t.Fatalf("PlanDay: %v", err)
	}
	client := &fakeClient{in: []Msg{{Kind: Hello}, {Kind: Pong}, {Kind: Pong}, {Kind: Pong}}}
	srv, ledger := newTestServer(7)

	got, err := srv.RunDay(cfg, []float64{2.0}, inReader{c: client}, outWriter{c: client})
	if err != nil {
		t.Fatalf("RunDay: %v", err)
	}
	if got.TotalActions != res.TotalActions || len(got.Sessions) != len(res.Sessions) {
		t.Errorf("RunDay result = %+v, want the PlanDay result %+v", got, res)
	}

	if st := ledger.Summarize(); st.Sessions != 1 || len(ledger.Entries)-2 != res.Sessions[0].Actions {
		t.Errorf("ledger = %d entries (%+v), want 1 session + %d actions", len(ledger.Entries), st, res.Sessions[0].Actions)
	}

	// Wire conversation: hello, then per session a ping + session-start, one
	// action line each, and finally shutdown.
	var lines []string
	for _, raw := range client.got {
		lines = append(lines, strings.TrimSuffix(string(raw), "\n"))
	}
	if len(lines) != 5+res.Sessions[0].Actions {
		t.Fatalf("server wrote %d lines (%s), want %d", len(lines), strings.Join(lines, " | "), 5+res.Sessions[0].Actions)
	}
	first := lines[0]
	if !strings.Contains(first, `"kind":"hello"`) {
		t.Errorf("first line = %s, want hello handshake", first)
	}
	if got := lines[len(lines)-1]; !strings.Contains(got, `"kind":"shutdown"`) {
		t.Errorf("last line = %s, want shutdown", got)
	}
}

func TestRunDayPlanFailurePropagates(t *testing.T) {
	cfg := core.Config{MinSessionHours: 1, MaxSessionHours: 8, Efficiency: 0.85}
	client := &fakeClient{}
	srv, _ := newTestServer(7)

	res, err := srv.RunDay(cfg, []float64{9.0}, inReader{c: client}, outWriter{c: client})
	if err == nil {
		t.Fatalf("RunDay with over-long session: want error, got nil")
	}
	if res != nil && len(res.Sessions) != 0 {
		t.Errorf("plan failure should carry no sessions, got %+v", res)
	}
	if !strings.Contains(err.Error(), "session 0") {
		t.Errorf("error %q should name the offending session", err)
	}
	if len(client.got) != 0 {
		t.Errorf("no wire traffic expected before planning, got %d lines", len(client.got))
	}
}

func TestRunDaySeededIsDeterministicAndConsistent(t *testing.T) {
	cfg := core.Config{MinSessionHours: 1, MaxSessionHours: 4, Efficiency: 0.85}
	run := func() *core.RunResult {
		client := &fakeClient{in: []Msg{{Kind: Hello}, {Kind: Pong}, {Kind: Pong}, {Kind: Pong}}}
		srv, _ := newTestServer(3)
		planner := core.NewSessionPlanner(cfg, rand.New(rand.NewSource(42)))
		res, err := srv.RunDaySeeded(planner, 3, inReader{c: client}, outWriter{c: client})
		if err != nil {
			t.Fatalf("RunDaySeeded: %v", err)
		}
		return res
	}
	a, b := run(), run()
	if a.TotalActions != b.TotalActions || len(a.Sessions) != len(b.Sessions) {
		t.Fatalf("same seed produced different days:\n%+v\n---\n%+v", a, b)
	}
	for i := range a.Sessions {
		if a.Sessions[i] != b.Sessions[i] {
			t.Errorf("session %d diverged: %+v vs %+v", i, a.Sessions[i], b.Sessions[i])
		}
	}
	// The drawn lengths must satisfy the never-identical-twice rule and stay
	// inside the configured range.
	for i, s := range a.Sessions {
		if s.Hours < cfg.MinSessionHours || s.Hours > cfg.MaxSessionHours {
			t.Errorf("session %d = %.1fh outside [%g, %g]", i, s.Hours, cfg.MinSessionHours, cfg.MaxSessionHours)
		}
		if i > 0 && s.Hours == a.Sessions[i-1].Hours {
			t.Errorf("session %d repeats previous length %.1fh", i, s.Hours)
		}
	}
	// And it must agree with drawing the lengths by hand and calling RunDay —
	// same seed, same day.
	client := &fakeClient{in: []Msg{{Kind: Hello}, {Kind: Pong}, {Kind: Pong}, {Kind: Pong}}}
	srv, _ := newTestServer(3)
	planner := core.NewSessionPlanner(cfg, rand.New(rand.NewSource(42)))
	lengths := planner.Sessions(3)
	res, err := srv.RunDay(cfg, lengths, inReader{c: client}, outWriter{c: client})
	if err != nil {
		t.Fatalf("RunDay: %v", err)
	}
	if res.TotalActions != a.TotalActions || len(res.Sessions) != len(a.Sessions) {
		t.Fatalf("RunDaySeeded and manual RunDay diverged:\n%+v\n---\n%+v", a, res)
	}
	for i := range res.Sessions {
		if res.Sessions[i].Hours != a.Sessions[i].Hours || res.Sessions[i].Actions != a.Sessions[i].Actions {
			t.Errorf("session %d differs between paths: %+v vs %+v", i, res.Sessions[i], a.Sessions[i])
		}
	}
}

func TestRunDayMiddayDropKeepsPartialLedger(t *testing.T) {
	cfg := core.Config{MinSessionHours: 0.01, MaxSessionHours: 8, Efficiency: 0.85}
	res, err := core.PlanDay(cfg, []float64{2.0})
	if err != nil {
		t.Fatalf("PlanDay: %v", err)
	}

	// Scenario one — the client dies before the first session starts: it
	// serves a partial hello line (no newline), then closes the read side.
	// With io.Pipe, ReadBytes returns the pending bytes first and reports the
	// close on the next read, so no goroutine ordering is involved: this is
	// exactly what a real dropping client looks like — a truncated final
	// line, then an EOF or broken pipe.
	pr, pw := io.Pipe()
	go func() {
		pw.Write([]byte(`{"kind":"hel`)) // partial line, no newline
		pw.Close()                       // read side sees the bytes, then EOF
	}()
	srv, ledger := newTestServer(7)

	got, err := srv.RunDay(cfg, []float64{2.0}, pr, io.Discard)
	if err == nil {
		t.Fatalf("RunDay with dropping client: want error, got nil")
	}
	// The drop must surface as a read-side failure naming where the
	// handshake broke. The plan is handed back either way — ban-appeal
	// evidence says what was *planned*. (The error here comes from parsing
	// the truncated hello line, so it is the JSON error, not a transport
	// sentinel: the wire layer's contract is that *any* read failure at
	// this point fails the run and names the handshake.)
	if !strings.Contains(err.Error(), "hello") {
		t.Errorf("error %q should name where the handshake broke", err)
	}
	// Nothing executed before the drop: the first session never started.
	if len(ledger.Entries) != 0 {
		t.Fatalf("ledger after pre-start drop = %+v, want empty", ledger.Entries)
	}
	// The partial run still hands back the full plan — ban-appeal evidence
	// should say what was *planned*, even if the client died at the door.
	if got == nil || len(got.Sessions) != len(res.Sessions) {
		t.Fatalf("partial run must still hand back the full plan, got %+v", got)
	}
	for i := range res.Sessions {
		if got.Sessions[i] != res.Sessions[i] || got.Sessions[i].Actions != res.Sessions[i].Actions {
			t.Errorf("plan session %d lost in partial run: got %+v, want %+v", i+1, got.Sessions[i], res.Sessions[i])
		}
	}
	if math.Abs(got.TotalHours-res.TotalHours) > 1e-9 || got.TotalActions != res.TotalActions {
		t.Errorf("plan totals lost in partial run: got %+v, want %+v", got, res)
	}

	// Scenario two — a mid-day drop with evidence on disk: session one runs
	// to completion (its keepalive pong is served), then the client goes away.
	// The fake client serves hello, session one's pong, and reports the wire
	// break when session two's keepalive ping is answered. Because Run pings at
	// the start of each session, a broken pipe mid-day surfaces exactly where
	// it happened: session one is fully on disk before the run fails. This
	// mirrors what io.Pipe produces when the peer process goes away, without
	// goroutine ordering to reason about.
	client2 := &fakeClient{in: []Msg{{Kind: Hello}, {Kind: Pong}}, err: io.ErrUnexpectedEOF}
	srv2, ledger2 := newTestServer(7)
	got2, err2 := srv2.RunDay(cfg, []float64{1.0, 1.0}, inReader{c: client2}, outWriter{c: client2})
	if err2 == nil {
		t.Fatalf("two-session drop: want error, got nil")
	}
	// A 1h session is what each of the two planned sessions gets here;
	// scenario one's 2h estimate is not needed for the partial-run count.
	r1, perr := core.PlanDay(cfg, []float64{1.0})
	if perr != nil {
		t.Fatalf("PlanDay(1h): %v", perr)
	}
	firstActions := r1.Sessions[0].Actions
	wantEntries := 1 + firstActions // session-start + every action of session one
	if len(ledger2.Entries) != wantEntries {
		t.Fatalf("mid-day drop: ledger = %d entries, want %d (session-start + session one's actions)",
			len(ledger2.Entries), wantEntries)
	}
	if ledger2.Entries[0].Kind != "session-start" {
		t.Errorf("first entry kind = %q, want session-start", ledger2.Entries[0].Kind)
	}
	last := ledger2.Entries[len(ledger2.Entries)-1]
	if last.Kind != "click" && last.Kind != "keypress" {
		t.Errorf("last entry kind = %q, want the first session's final action", last.Kind)
	}
	// The evidence must be usable: summarize what actually ran.
	st := ledger2.Summarize()
	if st.Sessions != 1 || st.Clicks+st.Keypresses != firstActions {
		t.Errorf("summary = %+v, want 1 session and %d actions", st, firstActions)
	}
	if got2 == nil || len(got2.Sessions) != 2 {
		t.Errorf("partial run must still hand back the full plan, got %+v", got2)
	}
}

func TestRunWithPersonalityEventsInterleaved(t *testing.T) {
	// Tight habit intervals so a short session (60 actions ≈ 1 min of wire
	// clock) schedules several events: means ~10-15s, small σ. A 40-second
	// chat mean would land past the action stream and get dropped, so keep it
	// well inside the ~70s budget a 60-action session leaves.
	pers := &core.PersonalityConfig{
		GlanceEvery: 12, GlanceSigma: 2,
		BankEvery: 15, BankSigma: 2,
		ChatEvery: 20, ChatSigma: 3,
	}

	client := &fakeClient{in: []Msg{{Kind: Hello}, {Kind: Pong}}}
	srv, ledger := newTestServer(42)
	srv.Personality = pers
	err := srv.Run(inReader{c: client}, outWriter{c: client}, []SessionPlan{{Hours: 1.0, Actions: 60}})
	if err != nil {
		t.Fatalf("Run with personality: %v", err)
	}

	st := ledger.Summarize()
	if st.Sessions != 1 {
		t.Errorf("summary sessions = %d, want 1", st.Sessions)
	}

	kinds := kindCounts(ledger)
	workActions := kinds["click"] + kinds["keypress"]
	if workActions != 60 {
		t.Errorf("work actions logged = %d, want exactly 60 (the plan's count)", workActions)
	}
	persEvents := len(ledger.Entries) - 2 - workActions // minus session-start and session-end markers; only habits remain
	if persEvents <= 0 {
		t.Fatalf("no personality events were emitted (kinds=%v), want habits in a 60-action session with tight intervals", kinds)
	}

	// The wire stream must carry every personality event as an action line
	// (with its habit detail), on top of the 60 plain work-action lines.
	lines := client.lines()
	withDetail := 0
	for _, l := range lines {
		switch {
		case strings.Contains(l, `"detail":"stats-tab glance`),
			strings.Contains(l, `"detail":"bank visit`),
			strings.Contains(l, `"detail":"chat 'ty'`):
			withDetail++
		}
	}
	if withDetail != persEvents {
		t.Errorf("wire carried %d personality action lines, ledger logged %d events — must match", withDetail, persEvents)
	}
	if got := actionLineCount(lines); got != 60+persEvents {
		t.Errorf("wire carried %d total action lines, want %d (60 work + %d personality)", got, 60+persEvents, persEvents)
	}

	// Every personality entry's detail must name its habit; a full 1h session
	// with these means must schedule all three classes.
	if kinds["glance"] == 0 || kinds["bank"] == 0 || kinds["chat"] == 0 {
		t.Errorf("habit class coverage = %v, want at least one glance, one bank visit and one chat", kinds)
	}
	for _, e := range ledger.Entries {
		if e.Kind == "glance" && !strings.Contains(e.Detail, "stats-tab glance") {
			t.Errorf("glance entry detail %q doesn't name the habit", e.Detail)
		}
	}

	// Nil Personality must behave exactly like before: only work actions.
	client2 := &fakeClient{in: []Msg{{Kind: Hello}, {Kind: Pong}}}
	srv2, ledger2 := newTestServer(42) // same seed -> same work-action stream
	err = srv2.Run(inReader{c: client2}, outWriter{c: client2}, []SessionPlan{{Hours: 1.0, Actions: 60}})
	if err != nil {
		t.Fatalf("Run without personality: %v", err)
	}
	baseline := kindCounts(ledger2)
	if baseline["click"]+baseline["keypress"] != 60 || baseline["glance"]+baseline["bank"]+baseline["chat"] != 0 {
		t.Errorf("baseline (nil Personality) kinds = %v, want exactly 60 work actions and zero habits", baseline)
	}
	// session-start + 60 actions + the confirmed session-end marker.
	if len(ledger2.Entries) != 1+60+1 {
		t.Errorf("baseline ledger = %d entries, want %d (session-start + 60 actions + session-end)", len(ledger2.Entries), 1+60+1)
	}
}
