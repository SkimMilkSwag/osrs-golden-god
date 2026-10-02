package core

import (
	"errors"
	"fmt"
	"math"
)

// Config holds the tunables for a bot run. Every field is documented because
// each one maps to a real anti-detection or efficiency knob from the OSRS
// botting playbook — see README.md, "The anti-ban playbook, condensed".
type Config struct {
	// World is the world number to log into (informational; a real bot passes
	// it to the client layer).
	World int

	// MinSessionHours / MaxSessionHours bound each session's duration. The
	// strongest single ban predictor is session length, so sessions are drawn
	// uniformly from this range and the sampler avoids repeating the same
	// duration twice in a row (see NewSessionPlanner).
	MinSessionHours float64
	MaxSessionHours float64

	// Efficiency is the fraction of theoretical max action speed the bot aims
	// for. The "85% rule": tick-perfect play for hours is itself an anomaly,
	// so bots deliberately sit below max efficiency. Must be in (0, 1].
	Efficiency float64

	// ClickSigmaMs is the standard deviation of the Gaussian noise applied to
	// each click's timing. Gaussian (not uniform) jitter is important: a
	// uniform-random delay is a distinctive behavioral fingerprint.
	ClickSigmaMs float64

	// DailyActionBudget caps actions per day, independent of session count,
	// so a long session can't quietly blow the day's volume. 0 disables the cap.
	DailyActionBudget int
}

var (
	// ErrSessionLengthTooShort is returned by a run whose planned session
	// length falls below MinSessionHours.
	ErrSessionLengthTooShort = errors.New("session length below minimum")
	// ErrSessionLengthTooLong is returned when a planned session exceeds the
	// configured maximum.
	ErrSessionLengthTooLong = errors.New("session length above maximum")
	// ErrEfficiencyOutOfRange is returned when Efficiency is not in (0, 1].
	ErrEfficiencyOutOfRange = errors.New("efficiency must be in (0, 1]")
)

// Validate checks the internal consistency of a Config. It is cheap and pure,
// so it can run in tests without touching any client.
func (c Config) Validate() error {
	if c.MinSessionHours <= 0 {
		return fmt.Errorf("min session hours must be > 0, got %g", c.MinSessionHours)
	}
	if c.MaxSessionHours < c.MinSessionHours {
		return fmt.Errorf("max session hours (%g) below min (%g)", c.MaxSessionHours, c.MinSessionHours)
	}
	if c.Efficiency <= 0 || c.Efficiency > 1 {
		return ErrEfficiencyOutOfRange
	}
	if c.ClickSigmaMs < 0 {
		return fmt.Errorf("click sigma must be >= 0, got %g", c.ClickSigmaMs)
	}
	return nil
}

// Session is a single planned login session.
type Session struct {
	// Hours is the planned duration of the session.
	Hours float64
	// Actions is the expected number of game actions this session will
	// perform, derived from the action rate and the 85% efficiency factor.
	Actions int
}

// RunResult aggregates what a full day of botting would look like given a
// Config. It is deterministic for a given set of inputs so tests can pin it.
type RunResult struct {
	Sessions     []Session
	TotalHours   float64
	TotalActions int
	// BudgetLeftOver is how many actions from DailyActionBudget were unused.
	// Zero when no daily budget was configured or the budget was fully spent.
	BudgetLeftOver int
}

// actionRatePerHour is a rough baseline of "meaningful" actions (clicks,
// keypresses, menu selections) per hour for a bank-stander / AFK combat bot.
// A human doing the same loop sustainably manages roughly this many without
// it looking scripted. The exact number is deliberately conservative; what
// matters is that sessions scale linearly with it so the budget math holds.
const actionRatePerHour = 60

// ActionRatePerHour exposes the baseline to other packages (e.g. the agent
// server, which schedules client actions at a human-sustainable rate).
func ActionRatePerHour() float64 {
	return actionRatePerHour
}

// PlanDay turns a Config plus an explicit list of session lengths (in hours,
// as a real bot would pick them at login time) into a RunResult. The lengths
// are validated against the config and converted into per-session action
// estimates using the efficiency factor.
func PlanDay(c Config, sessionHours []float64) (*RunResult, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	res := &RunResult{}
	remaining := c.DailyActionBudget
	for i, h := range sessionHours {
		if h < c.MinSessionHours {
			return nil, fmt.Errorf("session %d: %w (%.2fh)", i, ErrSessionLengthTooShort, h)
		}
		if h > c.MaxSessionHours {
			return nil, fmt.Errorf("session %d: %w (%.2fh)", i, ErrSessionLengthTooLong, h)
		}
		// Apply the 85%-style efficiency factor to the action estimate: a bot
		// at Efficiency e produces e * rate * hours actions for that session.
		actions := int(math.Round(h * actionRatePerHour * c.Efficiency))
		if c.DailyActionBudget > 0 && remaining < actions {
			actions = remaining
		}
		if c.DailyActionBudget > 0 {
			remaining -= actions
		}
		res.Sessions = append(res.Sessions, Session{Hours: h, Actions: actions})
		res.TotalHours += h
		res.TotalActions += actions
	}
	if c.DailyActionBudget > 0 {
		res.BudgetLeftOver = c.DailyActionBudget - res.TotalActions
	}
	return res, nil
}
