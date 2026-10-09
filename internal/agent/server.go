package agent

import (
	"errors"
	"fmt"
	"io"
	"math/rand"

	"github.com/SkimMilkSwag/osrs-golden-god/core"
)

// Server is the client-layer adapter: it speaks the stdio wire protocol to a
// game client (today, a RuneLite plugin that runs the agent server) and feeds
// every executed action back into a ledger. The behavior layer stays in
// core — the Server only *executes* what PlanDay decided, so swapping clients
// never means touching the planning math.
type Server struct {
	// Ledger receives one Entry per executed action (and session-start /
	// session-end). Injected so tests can run against an in-memory ledger and
	// inspect it after Run returns.
	Ledger *core.Ledger

	// ActionKinds is the pool of action classes the client performs, used
	// for the detail strings on ledger entries (e.g. "click", "keypress").
	// Defaults to a small bank-stander-ish set when empty.
	ActionKinds []string

	// Personality, when non-nil, schedules the human-habit events (stats-tab
	// glances, bank visits, chat) that are interleaved into each session's
	// action stream and reported to the client as their own Action lines. A
	// nil Personality disables them — the executor then behaves exactly as
	// it did before this knob existed, which keeps existing wire tests stable.
	Personality *core.PersonalityConfig

	rng *rand.Rand
}

// NewServer builds an executor around the given ledger. The RNG is injectable
// so tests are deterministic.
func NewServer(ledger *core.Ledger, rng *rand.Rand) *Server {
	s := &Server{Ledger: ledger, rng: rng}
	if s.ActionKinds == nil {
		s.ActionKinds = defaultActionKinds
	}
	return s
}

// defaultActionKinds is the fallback pool of action classes for detail
// strings when none are configured — a minimal bank-stander-ish mix.
var defaultActionKinds = []string{"click", "keypress"}

// ErrNoSessions is returned by Run when the plan carries no sessions.
var ErrNoSessions = errors.New("plan has no sessions")

// SessionPlan describes one session to execute. It mirrors the shape of
// core.Session without importing package main (the wire layer must stay free
// of CLI types so a future client can speak the same protocol).
type SessionPlan struct {
	Hours   float64
	Actions int
}

// FromRun converts a PlanDay result into the session plans Run executes. The
// conversion is the only place where the behavior layer's output meets the
// wire contract, which keeps both sides independently testable.
func FromRun(r *core.RunResult) []SessionPlan {
	out := make([]SessionPlan, len(r.Sessions))
	for i, s := range r.Sessions {
		out[i] = SessionPlan{Hours: s.Hours, Actions: s.Actions}
	}
	return out
}

// RunDay plans a day with the given session lengths and executes it against
// the client in one call — the common path where nothing else owns the
// planning. It returns the plan alongside the error so a partial run (a
// client drop mid-day) still hands back what was planned, which is exactly
// the evidence a ban appeal wants.
func (s *Server) RunDay(cfg core.Config, sessionHours []float64, in io.Reader, out io.Writer) (*core.RunResult, error) {
	res, err := core.PlanDay(cfg, sessionHours)
	if err != nil {
		return res, fmt.Errorf("plan: %w", err)
	}
	if err := s.Run(in, out, FromRun(res)); err != nil {
		return res, err
	}
	return res, nil
}

// RunDaySeeded is RunDay for callers that also own session-length drawing: it
// draws the lengths with an injectable planner (so tests and demos are
// deterministic) and then plans and executes exactly as RunDay does.
func (s *Server) RunDaySeeded(planner *core.SessionPlanner, n int, in io.Reader, out io.Writer) (*core.RunResult, error) {
	return s.RunDay(planner.Config(), planner.Sessions(n), in, out)
}

// Run drives the full conversation with the client: hello handshake, then for
// each planned session a session-start / action* / session-end cycle, ending
// with shutdown. Actions are generated locally and logged to the ledger with
// strictly increasing ticks; a misbehaving peer (wrong kind on the wire) fails
// the run instead of being silently ignored.
//
// A session's end is only *confirmed* once the next session actually begins —
// like a real client that never logs out mid-day, you can't know a session
// ended until continuity is proven. So runSession performs its start + actions
// and reports where the end will land, and Run appends that session-end entry
// right before starting the next session (or after the last one on clean
// completion). A mid-day drop therefore leaves the partial ledger ending on
// the last completed action, not a session-end that was never confirmed.
//
// The tick model is the OSRS game clock: 10 ticks per second, 600ms each. One
// action consumes one second of game time — at the 60 actions/hour baseline
// (core.ActionRatePerHour) that is roughly the pacing PlanDay estimates.
func (s *Server) Run(in io.Reader, out io.Writer, sessions []SessionPlan) error {
	if len(sessions) == 0 {
		return ErrNoSessions
	}
	if s.Ledger == nil {
		return errors.New("ledger is nil")
	}

	// tick is the game tick the current session is at; sessions continue from
	// where the previous one left off, like a real client that never logs out
	// mid-day.
	tick := 0

	// Handshake: we send hello, expect the peer's hello back.
	if err := WriteMsg(out, &Msg{Kind: Hello}); err != nil {
		return fmt.Errorf("write hello: %w", err)
	}
	hello, err := ReadMsg(in)
	if err != nil {
		return fmt.Errorf("read hello: %w", err)
	}
	if hello.Kind != Hello {
		return fmt.Errorf("expected hello first, got %q", hello.Kind)
	}

	var (
		pendingEnd  int64
		pendingHrs  float64
		havePending bool
	)
	for i, sp := range sessions {
		if sp.Actions < 0 {
			return fmt.Errorf("session %d: negative action count (%d)", i+1, sp.Actions)
		}
		end, err := s.runSession(in, out, &tick, sp, havePending, pendingEnd, pendingHrs)
		if err != nil {
			return fmt.Errorf("session %d: %w", i+1, err)
		}
		pendingEnd, pendingHrs, havePending = end, sp.Hours, true
	}

	// Clean completion: the last session's end is confirmed at shutdown.
	if havePending {
		if err := s.Ledger.Append(core.Entry{Tick: int(pendingEnd), Kind: "session-end", Detail: fmt.Sprintf("%.1fh", pendingHrs)}); err != nil {
			return fmt.Errorf("confirm final session end: %w", err)
		}
	}

	if err := WriteMsg(out, &Msg{Kind: Shutdown}); err != nil {
		return fmt.Errorf("write shutdown: %w", err)
	}
	return nil
}

// runSession executes one session. It first confirms the PREVIOUS session's
// end — but only once this session's keepalive pong is received, proving the
// client survived to start again (a mid-day drop at this keepalive therefore
// leaves the prior session's end unconfirmed, so the ledger ends on its last
// action). Then it announces the session and performs its actions (each logged
// to the ledger and echoed to the client as an Action message), interleaving
// any scheduled personality events in game-tick order. It returns the tick at
// which this session's end marker will land. The wire clock advances one
// second for every action; the returned end sits one second after the last
// action (or one second after the start for an empty, zero-action session).
func (s *Server) runSession(in io.Reader, out io.Writer, tick *int, sp SessionPlan, hadPrev bool, prevEnd int64, prevHrs float64) (int64, error) {
	// Keepalive: a client that died between sessions won't answer its ping, so
	// a mid-day drop fails the run at the session boundary with the partial
	// ledger still intact — exactly the evidence a ban appeal wants.
	if err := WriteMsg(out, &Msg{Kind: Ping}); err != nil {
		return 0, fmt.Errorf("write ping: %w", err)
	}
	pong, err := ReadMsg(in)
	if err != nil {
		return 0, fmt.Errorf("read pong: %w", err)
	}
	if pong.Kind != Pong {
		return 0, fmt.Errorf("expected pong before session start, got %q", pong.Kind)
	}

	// The keepalive pong just proved the client survived between sessions, so
	// the previous session's end is now confirmed and can be written to the
	// ledger. (If the client had died here, we'd have returned above and its
	// prior session would stay unconfirmed.)
	if hadPrev {
		if err := s.Ledger.Append(core.Entry{Tick: int(prevEnd), Kind: "session-end", Detail: fmt.Sprintf("%.1fh", prevHrs)}); err != nil {
			return 0, fmt.Errorf("confirm previous session end: %w", err)
		}
	}

	if err := WriteMsg(out, &Msg{Kind: SessionStart, Session: &SessionSpec{Hours: sp.Hours, Actions: sp.Actions}}); err != nil {
		return 0, fmt.Errorf("write session-start: %w", err)
	}

	if err := s.Ledger.Append(core.Entry{Tick: *tick, Kind: "session-start", Detail: fmt.Sprintf("%.1fh, ~%d actions", sp.Hours, sp.Actions)}); err != nil {
		return 0, fmt.Errorf("ledger session-start: %w", err)
	}

	// Personality events are scheduled for the whole session up front, in the
	// generator's own game-tick clock (session start = tick 0). Work actions
	// advance the wire clock one second each; an event is emitted the moment
	// the wire clock crosses its scheduled second. Events scheduled past the
	// session's action stream (long habits vs short sessions) are dropped:
	// they simply didn't happen this session.
	var pers []core.PersonalityEvent
	if s.Personality != nil {
		gen := core.NewPersonalityGenerator(*s.Personality, rand.New(rand.NewSource(s.rng.Int63())), sp.Hours)
		pers = gen.Schedule()
	}
	nextPers := 0
	for a := 0; a < sp.Actions; a++ {
		if err := WriteMsg(out, &Msg{Kind: Action}); err != nil {
			return 0, fmt.Errorf("write action %d: %w", a+1, err)
		}
		*tick += 10 // one second of game time per action
		kind := s.ActionKinds[s.rng.Intn(len(s.ActionKinds))]
		detail := fmt.Sprintf("%s #%d", kind, a+1)
		if err := s.Ledger.Append(core.Entry{Tick: *tick, Kind: kind, Detail: detail}); err != nil {
			return 0, fmt.Errorf("ledger action %d: %w", a+1, err)
		}

		// Emit every personality event whose scheduled second has now passed.
		// Each event consumes a fresh second of game time: the wire clock only
		// ever moves forward, and a habit that "happens at" the same moment as
		// another (two events crossing in one action) still occupies its own
		// tick-second when logged — otherwise the ledger's strict-tick rule
		// would reject the second append at the same tick.
		for nextPers < len(pers) && pers[nextPers].Tick <= *tick {
			ev := pers[nextPers]
			nextPers++
			*tick += 10 // this event takes the next second of game time
			if err := WriteMsg(out, &Msg{Kind: Action, Tick: *tick, Detail: ev.Detail}); err != nil {
				return 0, fmt.Errorf("write personality event: %w", err)
			}
			if err := s.Ledger.Append(core.Entry{Tick: *tick, Kind: ev.Kind, Detail: ev.Detail}); err != nil {
				return 0, fmt.Errorf("ledger personality event: %w", err)
			}
		}
	}

	// The session-end marker lands one full second AFTER the last action (the
	// "one tick-second after the last action" design): for a non-empty session
	// that is lastAction + 10, and the ledger's tick clock then advances past it
	// so the NEXT session's start is strictly greater than this one's end. An
	// empty (zero-action) session has no last action; its end marker sits one
	// second after its own start, still keeping the sequence strictly increasing.
	lastAction := *tick // *tick already reflects every action performed
	end := int64(lastAction + 10)
	if sp.Actions == 0 {
		end = int64(*tick) + 10 // no actions ran: end one second after session-start
	}
	*tick = lastAction + 20 // leave the clock past this session so the next start > this end
	if err := WriteMsg(out, &Msg{Kind: SessionEnd}); err != nil {
		return 0, fmt.Errorf("write session-end: %w", err)
	}
	return end, nil
}
