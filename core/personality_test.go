package core

import (
	"math/rand"
	"testing"
)

func validPersonalityConfig() PersonalityConfig {
	return PersonalityConfig{
		GlanceEvery: 300, GlanceSigma: 60, // ~5min mean, 1min σ
		BankEvery: 900, BankSigma: 120, // ~15min mean, 2min σ
		ChatEvery: 1200, ChatSigma: 300, // ~20min mean, 5min σ
	}
}

func TestPersonalityConfigValidate(t *testing.T) {
	if err := validPersonalityConfig().Validate(); err != nil {
		t.Fatalf("expected valid config to pass, got %v", err)
	}
	bad := []struct {
		name   string
		mutate func(*PersonalityConfig)
	}{
		{"glance mean zero", func(c *PersonalityConfig) { c.GlanceEvery = 0 }},
		{"glance sigma negative", func(c *PersonalityConfig) { c.GlanceSigma = -1 }},
		{"bank mean negative", func(c *PersonalityConfig) { c.BankEvery = -5 }},
		{"chat sigma negative", func(c *PersonalityConfig) { c.ChatSigma = -0.1 }},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			c := validPersonalityConfig()
			tt.mutate(&c)
			if err := c.Validate(); err == nil {
				t.Fatal("Validate() = nil, want error")
			}
		})
	}
}

func TestPersonalityScheduleStrictlyIncreasingTicks(t *testing.T) {
	g := NewPersonalityGenerator(validPersonalityConfig(), rand.New(rand.NewSource(7)), 1.0)
	sched := g.Schedule()
	if len(sched) == 0 {
		t.Fatal("Schedule() returned no events for a 1h session")
	}
	for i, ev := range sched {
		if ev.Tick <= 0 {
			t.Fatalf("event %d tick = %d, want > 0", i, ev.Tick)
		}
		if i > 0 && ev.Tick <= sched[i-1].Tick {
			t.Fatalf("event %d tick %d not strictly after event %d tick %d",
				i, ev.Tick, i-1, sched[i-1].Tick)
		}
		if ev.Tick > g.SessionEndTick() {
			t.Fatalf("event %d at tick %d is past the session end (%d)",
				i, ev.Tick, g.SessionEndTick())
		}
		switch ev.Kind {
		case "glance", "bank", "chat":
		default:
			t.Fatalf("event %d has unknown kind %q", i, ev.Kind)
		}
		if ev.Detail == "" {
			t.Errorf("event %d has empty detail", i)
		}
	}
}

func TestPersonalityScheduleInterleavesAllClasses(t *testing.T) {
	// Round-robin must surface all three classes in a long-enough session:
	// with these intervals a 1h session schedules at least one of each.
	g := NewPersonalityGenerator(validPersonalityConfig(), rand.New(rand.NewSource(7)), 1.0)
	sched := g.Schedule()
	seen := map[string]int{}
	for _, ev := range sched {
		seen[ev.Kind]++
	}
	for _, k := range []string{"glance", "bank", "chat"} {
		if seen[k] == 0 {
			t.Errorf("no %q events scheduled (saw %v)", k, seen)
		}
	}
	// The first event must be the first class in the rotation (glance): a
	// fresh generator starts at index zero.
	if sched[0].Kind != "glance" {
		t.Errorf("first event kind = %q, want glance (rotation starts there)", sched[0].Kind)
	}
}

func TestPersonalityScheduleSeededDeterminism(t *testing.T) {
	cfg := validPersonalityConfig()
	mk := func(seed int64) []PersonalityEvent {
		g := NewPersonalityGenerator(cfg, rand.New(rand.NewSource(seed)), 2.0)
		return g.Schedule()
	}
	a, b := mk(99), mk(99)
	if len(a) != len(b) {
		t.Fatalf("same seed produced %d vs %d events", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("seeded schedule diverged at %d: %+v vs %+v", i, a[i], b[i])
		}
	}
}

func TestPersonalityScheduleScalesWithSessionLength(t *testing.T) {
	cfg := validPersonalityConfig()
	short := NewPersonalityGenerator(cfg, rand.New(rand.NewSource(5)), 0.25).Schedule()
	long := NewPersonalityGenerator(cfg, rand.New(rand.NewSource(5)), 4.0).Schedule()
	if len(long) <= len(short) {
		t.Errorf("4h session scheduled %d events, 15m session scheduled %d — longer sessions should schedule more", len(long), len(short))
	}
}

// TestPersonalityScheduleFeedsLedger drives a real Ledger with the generated
// schedule to prove the generator's output is directly loggable: a
// PersonalityEvent maps one-for-one to an Entry, ticks stay strictly
// increasing through Append, and Summarize counts the new kinds.
func TestPersonalityScheduleFeedsLedger(t *testing.T) {
	// Use tight intervals so even a short session schedules all three
	// classes (round-robin guarantees one of each per three events).
	cfg := PersonalityConfig{
		GlanceEvery: 30, GlanceSigma: 5,
		BankEvery: 45, BankSigma: 10,
		ChatEvery: 60, ChatSigma: 20,
	}
	g := NewPersonalityGenerator(cfg, rand.New(rand.NewSource(11)), 0.1) // 6min session
	sched := g.Schedule()
	if len(sched) < 3 {
		t.Fatalf("expected at least 3 events in a 6min session with tight intervals, got %d", len(sched))
	}

	l := &Ledger{}
	for _, ev := range sched {
		if err := l.Append(Entry{Tick: ev.Tick, Kind: ev.Kind, Detail: ev.Detail}); err != nil {
			t.Fatalf("Append(%+v) failed: %v", ev, err)
		}
	}
	st := l.Summarize()
	if st.Banks == 0 || st.Chats == 0 {
		t.Errorf("summary = %+v, want bank and chat events counted", st)
	}
	if st.DurationTicks <= 0 {
		t.Errorf("DurationTicks = %d, want > 0", st.DurationTicks)
	}
}
