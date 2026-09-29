package main

import (
	"math"
	"math/rand"
)

// SessionPlanner picks session lengths that look human: drawn from the
// configured range, and never identical to the previous session. Identical
// session lengths back-to-back are a cheap cluster signal — the playbook says
// "never identical twice".
type SessionPlanner struct {
	cfg       Config
	rng       *rand.Rand
	lastHours float64
	haveLast  bool
}

// NewSessionPlanner builds a planner with the given RNG (injectable so tests
// can seed it).
func NewSessionPlanner(cfg Config, rng *rand.Rand) *SessionPlanner {
	return &SessionPlanner{cfg: cfg, rng: rng}
}

// Next draws the duration of the next session in hours. It resamples while the
// drawn value rounds to the same length as the previous one (to 0.1h
// precision), with a bounded retry count so a degenerate config can't hang it.
func (p *SessionPlanner) Next() float64 {
	for attempt := 0; attempt < 32; attempt++ {
		h := p.cfg.MinSessionHours + p.rng.Float64()*(p.cfg.MaxSessionHours-p.cfg.MinSessionHours)
		h = math.Round(h*10) / 10 // snap to 0.1h — no one plans "3.47 hours"
		if !p.haveLast || h != p.lastHours {
			p.lastHours = h
			p.haveLast = true
			return h
		}
	}
	return p.lastHours
}
