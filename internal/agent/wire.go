package agent

import "errors"

// Wire protocol between the behavior layer and a game client. The client is a
// RuneLite plugin (or any native client) that runs an agent-server over stdio:
// the behavior layer is the parent process, one JSON object per line in each
// direction. Keeping the protocol this small on purpose — it only needs to
// carry what PlanDay decides (a session's duration and action volume) and what
// the ledger records back (a ticked event stream). Everything richer lives in
// the behavior layer so the client can be swapped without touching it.

// Kind is a message tag. The strings are the wire format — they must not drift
// (same reason Entry's json tags are pinned: both sides agree on them).
type Kind string

const (
	// Hello is sent by the server first: version + protocol check.
	Hello Kind = "hello"
	// SessionStart begins a session of the given duration.
	SessionStart Kind = "session-start"
	// SessionEnd ends the current session.
	SessionEnd Kind = "session-end"
	// Action is one game action executed by the client (click, keypress,
	// bank visit, chat message). Ticked so the ledger can order it.
	Action Kind = "action"
	// Stats is a server-initiated snapshot request; the server answers with
	// its current state.
	Stats Kind = "stats"
	// Shutdown terminates the connection after an in-flight reply.
	Shutdown Kind = "shutdown"
	// Ping is a keepalive probe answered with Pong.
	Ping Kind = "ping"
	// Pong answers a Ping (carries the echoed payload, if any).
	Pong Kind = "pong"
)

var (
	// ErrUnknownKind is returned by Unmarshal for a message whose kind tag is
	// not one of the constants above.
	ErrUnknownKind = errors.New("unknown wire kind")
	// ErrMissingSessionStart is returned when SessionEnd or Action arrives
	// with no active session.
	ErrMissingSessionStart = errors.New("session-start required first")
)

// Msg is the envelope for every line in either direction. JSON field names are
// the wire format.
type Msg struct {
	Kind Kind `json:"kind"`
	// Session is set on SessionStart: the planned duration and action count
	// for the session about to begin (from PlanDay's output).
	Session *SessionSpec `json:"session,omitempty"`
	// Tick / Detail are set on Action: the game tick the action happened at
	// and a human-readable description (ledger-friendly).
	Tick   int    `json:"tick,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// SessionSpec is what the behavior layer tells the client before a session:
// how long to stay in and roughly how many actions it will drive.
type SessionSpec struct {
	Hours   float64 `json:"hours"`
	Actions int     `json:"actions"`
}
