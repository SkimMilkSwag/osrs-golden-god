package main

import (
	"errors"
	"math/rand"
	"testing"
)

func validConfig() Config {
	return Config{
		World:             1,
		MinSessionHours:   2,
		MaxSessionHours:   6,
		Efficiency:        0.85,
		ClickSigmaMs:      40,
		DailyActionBudget: 0,
	}
}

func TestValidateAcceptsGoodConfig(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("expected valid config to pass, got %v", err)
	}
}

func TestValidateRejectsBadConfigs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		want   error
	}{
		{"min hours zero", func(c *Config) { c.MinSessionHours = 0 }, nil},
		{"max below min", func(c *Config) { c.MaxSessionHours = 1 }, nil},
		{"efficiency zero", func(c *Config) { c.Efficiency = 0 }, ErrEfficiencyOutOfRange},
		{"efficiency over one", func(c *Config) { c.Efficiency = 1.5 }, ErrEfficiencyOutOfRange},
		{"negative click sigma", func(c *Config) { c.ClickSigmaMs = -1 }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validConfig()
			tt.mutate(&c)
			err := c.Validate()
			if tt.want != nil {
				if !errors.Is(err, tt.want) {
					t.Fatalf("Validate() = %v, want errors.Is %v", err, tt.want)
				}
				return
			}
			if err == nil {
				t.Fatal("Validate() = nil, want error")
			}
		})
	}
}

func TestPlanDayActionEstimate(t *testing.T) {
	c := validConfig() // efficiency 0.85, rate 60/h
	res, err := PlanDay(c, []float64{2, 4})
	if err != nil {
		t.Fatalf("PlanDay: %v", err)
	}
	if len(res.Sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(res.Sessions))
	}
	// 2h at 0.85 * 60 = 102; 4h = 204. Total 306.
	if res.TotalActions != 306 {
		t.Errorf("TotalActions = %d, want 306", res.TotalActions)
	}
	if res.Sessions[0].Actions != 102 {
		t.Errorf("session 0 actions = %d, want 102", res.Sessions[0].Actions)
	}
	if res.TotalHours != 6 {
		t.Errorf("TotalHours = %g, want 6", res.TotalHours)
	}
}

func TestPlanDaySessionBounds(t *testing.T) {
	c := validConfig() // min 2h, max 6h
	if _, err := PlanDay(c, []float64{1.9}); !errors.Is(err, ErrSessionLengthTooShort) {
		t.Errorf("short session: got %v, want ErrSessionLengthTooShort", err)
	}
	if _, err := PlanDay(c, []float64{6.1}); !errors.Is(err, ErrSessionLengthTooLong) {
		t.Errorf("long session: got %v, want ErrSessionLengthTooLong", err)
	}
}

func TestPlanDayDailyBudgetCapsActions(t *testing.T) {
	c := validConfig()
	c.DailyActionBudget = 150 // less than the 306 the plan would otherwise produce
	res, err := PlanDay(c, []float64{2, 4})
	if err != nil {
		t.Fatalf("PlanDay: %v", err)
	}
	if res.TotalActions != 150 {
		t.Errorf("TotalActions = %d, want budget cap of 150", res.TotalActions)
	}
	if res.BudgetLeftOver != 0 {
		t.Errorf("BudgetLeftOver = %d, want 0 (fully spent)", res.BudgetLeftOver)
	}
}

func TestPlanDayBudgetLeftOver(t *testing.T) {
	c := validConfig()
	c.DailyActionBudget = 500
	res, err := PlanDay(c, []float64{2, 4}) // produces 306
	if err != nil {
		t.Fatalf("PlanDay: %v", err)
	}
	if res.BudgetLeftOver != 194 {
		t.Errorf("BudgetLeftOver = %d, want 194", res.BudgetLeftOver)
	}
}

func TestPlanDayInvalidConfigPropagates(t *testing.T) {
	c := validConfig()
	c.Efficiency = 2 // out of range
	if _, err := PlanDay(c, []float64{3}); !errors.Is(err, ErrEfficiencyOutOfRange) {
		t.Errorf("got %v, want ErrEfficiencyOutOfRange", err)
	}
}

func TestSessionPlannerNeverRepeats(t *testing.T) {
	c := validConfig() // 2..6 hours, snapped to 0.1h -> only 41 distinct values
	p := NewSessionPlanner(c, rand.New(rand.NewSource(42)))
	prev := 0.0
	for i := 0; i < 500; i++ {
		h := p.Next()
		if h < c.MinSessionHours || h > c.MaxSessionHours {
			t.Fatalf("session %d = %g outside [%g, %g]", i, h, c.MinSessionHours, c.MaxSessionHours)
		}
		if h == prev {
			t.Fatalf("session %d repeated previous length %g", i, h)
		}
		prev = h
	}
}

func TestSessionPlannerSeededDeterminism(t *testing.T) {
	c := validConfig()
	mk := func(seed int64) []float64 {
		p := NewSessionPlanner(c, rand.New(rand.NewSource(seed)))
		out := make([]float64, 20)
		for i := range out {
			out[i] = p.Next()
		}
		return out
	}
	a, b := mk(7), mk(7)
	if len(a) != len(b) {
		t.Fatalf("length mismatch: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("seeded planner diverged at %d: %g vs %g", i, a[i], b[i])
		}
	}
}
