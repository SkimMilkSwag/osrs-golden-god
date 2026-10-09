package core

import (
	"fmt"
	"math"
	"math/rand"
)

// PersonalityConfig tunes the personality-actions generator. Each pair of
// fields maps to a real anti-detection knob: a bot that only ever performs
// its task is a distinctive behavioral fingerprint, so real bots sprinkle in
// "human" actions — glancing at the stats tab, saying ty after receiving
// gear, visiting the bank between kills — at irregular intervals.
//
// The intervals are Gaussian, not uniform: a uniform random delay between
// events is itself a fingerprint (the same reason click timing uses Gaussian
// jitter, see Config.ClickSigmaMs). A Gaussian cluster around a mean interval
// reads as "a person who does this roughly every N minutes", which is what a
// human actually produces.
type PersonalityConfig struct {
	// GlanceEvery / GlanceSigma are the mean and σ (in seconds) between
	// stats-tab glances — the idle habit of checking how the session is going.
	GlanceEvery float64
	GlanceSigma float64

	// BankEvery / BankSigma are the mean and σ (in seconds) between bank
	// visits (the task itself, but logged as a distinct, rarer event class).
	BankEvery float64
	BankSigma float64

	// ChatEvery / ChatSigma are the mean and σ (in seconds) between chat
	// messages ("ty" after receiving gear, "nice drop", etc.).
	ChatEvery float64
	ChatSigma float64
}

// Validate checks the PersonalityConfig's internal consistency. Each mean
// interval must be positive (a non-positive mean means "never" or "always",
// neither of which is a sane human habit) and each σ must be non-negative.
func (c PersonalityConfig) Validate() error {
	if c.GlanceEvery <= 0 || c.GlanceSigma < 0 {
		return fmt.Errorf("glance interval: mean must be > 0 (got %g), sigma >= 0 (got %g)", c.GlanceEvery, c.GlanceSigma)
	}
	if c.BankEvery <= 0 || c.BankSigma < 0 {
		return fmt.Errorf("bank interval: mean must be > 0 (got %g), sigma >= 0 (got %g)", c.BankEvery, c.BankSigma)
	}
	if c.ChatEvery <= 0 || c.ChatSigma < 0 {
		return fmt.Errorf("chat interval: mean must be > 0 (got %g), sigma >= 0 (got %g)", c.ChatEvery, c.ChatSigma)
	}
	return nil
}

// PersonalityEvent is one scheduled human action. Kind matches a ledger
// entry kind ("glance", "bank", "chat"), so logging an event is a direct
// Ledger.Append; Detail carries the human-readable context for the ledger's
// ban-appeal evidence.
type PersonalityEvent struct {
	Tick   int
	Kind   string
	Detail string
}

// PersonalityGenerator schedules personality actions over one session of the
// given length. It is deterministic for a given RNG, so a planned day's
// human-like noise can be pinned in tests exactly like session lengths can
// (see NewSessionPlanner). The game clock it drives is the OSRS tick: 600ms
// each, 10 per second — the same clock the ledger and the agent server use.
type PersonalityGenerator struct {
	cfg   PersonalityConfig
	rng   *rand.Rand
	tick  int // current position on the session's game-tick clock
	end   int // tick at which the session ends
	class int // index of the next event class (round-robin rotation)
}

// NewPersonalityGenerator builds a generator with an injectable RNG (tests
// seed it) and a session length in hours.
func NewPersonalityGenerator(cfg PersonalityConfig, rng *rand.Rand, sessionHours float64) *PersonalityGenerator {
	end := int(math.Round(sessionHours * 3600 * 10)) // hours -> ticks at 10/s
	return &PersonalityGenerator{cfg: cfg, rng: rng, end: end}
}

// SessionEndTick returns the game tick at which the session ends.
func (g *PersonalityGenerator) SessionEndTick() int { return g.end }

// eventClass is one of the three human-action classes the generator rotates
// through in round-robin order. Round-robin (rather than drawing a class per
// event) keeps the schedule interleaved: glance, bank, chat, glance, ... so a
// session's ledger never shows a long stretch of only one habit.
type eventClass struct {
	kind   string
	mean   float64
	sigma  float64
	detail string
}

var personalityClasses = []eventClass{
	{"glance", 0, 0, "stats-tab glance"},
	{"bank", 0, 0, "bank visit"},
	{"chat", 0, 0, "chat 'ty'"},
}

// Next advances the internal clock by one Gaussian-drawn interval and returns
// the next scheduled event. The class is chosen by round-robin (see
// personalityClasses); its interval mean/σ come from the generator's config.
// A near-zero draw is clamped to a 10-second floor: two events in the same
// second reads as double-logging, not as a human habit. The first event lands
// a full drawn interval after the session start — nobody opens the stats tab
// at second zero.
func (g *PersonalityGenerator) Next() PersonalityEvent {
	cls := personalityClasses[g.class%len(personalityClasses)]
	g.class++

	var mean, sigma float64
	switch cls.kind {
	case "glance":
		mean, sigma = g.cfg.GlanceEvery, g.cfg.GlanceSigma
	case "bank":
		mean, sigma = g.cfg.BankEvery, g.cfg.BankSigma
	case "chat":
		mean, sigma = g.cfg.ChatEvery, g.cfg.ChatSigma
	}

	secs := mean + sigma*g.rng.NormFloat64()
	if secs < 10 {
		secs = 10
	}
	g.tick += int(math.Round(secs * 10)) // seconds -> ticks (10 per second)

	return PersonalityEvent{
		Tick:   g.tick,
		Kind:   cls.kind,
		Detail: fmt.Sprintf("%s at t=%ds", cls.detail, g.tick/10),
	}
}

// Schedule returns every personality event that falls inside the session, as
// a strictly-increasing-tick list ready to interleave with work actions. It
// stops once the clock passes the session's end (the length it was built
// with), so callers get exactly the events that fit — no off-session tail.
func (g *PersonalityGenerator) Schedule() []PersonalityEvent {
	var out []PersonalityEvent
	for len(out) < 10000 { // safety bound; a real session never schedules this many
		ev := g.Next()
		if ev.Tick > g.end {
			break
		}
		out = append(out, ev)
	}
	return out
}
