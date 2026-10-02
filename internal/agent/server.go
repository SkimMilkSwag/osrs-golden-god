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

// Run drives the full conversation with the client: hello handshake, then for
// each planned session a session-start / action* / session-end cycle, ending
// with shutdown. Actions are generated locally and logged to the ledger with
// strictly increasing ticks; a misbehaving peer (wrong kind on the wire) fails
// the run instead of being silently ignored.
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
	kinds := s.ActionKinds
	if len(kinds) == 0 {
		kinds = defaultActionKinds
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

	for i, sp := range sessions {
		if sp.Actions < 0 {
			return fmt.Errorf("session %d: negative action count (%d)", i+1, sp.Actions)
		}
		if err := s.runSession(out, &tick, sp); err != nil {
			return fmt.Errorf("session %d: %w", i+1, err)
		}
	}

	if err := WriteMsg(out, &Msg{Kind: Shutdown}); err != nil {
		return fmt.Errorf("write shutdown: %w", err)
	}
	return nil
}

// runSession executes one session: announces it, performs its actions (each
// logged to the ledger and echoed to the client as an Action message), then
// closes it out. The session-start is logged at the tick the session begins;
// the session-end one tick-second after the last action.
func (s *Server) runSession(out io.Writer, tick *int, sp SessionPlan) error {
	if err := WriteMsg(out, &Msg{Kind: SessionStart, Session: &SessionSpec{Hours: sp.Hours, Actions: sp.Actions}}); err != nil {
		return fmt.Errorf("write session-start: %w", err)
	}

	if err := s.Ledger.Append(core.Entry{Tick: *tick, Kind: "session-start", Detail: fmt.Sprintf("%.1fh, ~%d actions", sp.Hours, sp.Actions)}); err != nil {
		return fmt.Errorf("ledger session-start: %w", err)
	}

	for a := 0; a < sp.Actions; a++ {
		*tick += 10 // one second of game time per action
		kind := s.ActionKinds[s.rng.Intn(len(s.ActionKinds))]
		detail := fmt.Sprintf("%s #%d", kind, a+1)
		if err := s.Ledger.Append(core.Entry{Tick: *tick, Kind: kind, Detail: detail}); err != nil {
			return fmt.Errorf("ledger action %d: %w", a+1, err)
		}
		if err := WriteMsg(out, &Msg{Kind: Action, Tick: *tick, Detail: detail}); err != nil {
			return fmt.Errorf("write action %d: %w", a+1, err)
		}
	}

	end := *tick + 10 // session-end is logged one second after the last action
	*tick = end
	if err := WriteMsg(out, &Msg{Kind: SessionEnd}); err != nil {
		return fmt.Errorf("write session-end: %w", err)
	}
	if err := s.Ledger.Append(core.Entry{Tick: end, Kind: "session-end", Detail: fmt.Sprintf("%.1fh", sp.Hours)}); err != nil {
		return fmt.Errorf("ledger session-end: %w", err)
	}
	return nil
}
