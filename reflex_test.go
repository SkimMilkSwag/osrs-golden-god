package main

import (
	"math/rand"
	"testing"
)

// newTestReflex builds a reflex with a deterministic RNG and a character that
// is hard to kill (200 max HP, full health, 1 prayer point, 3 potions).
func newTestReflex(seed int64) *Reflex {
	return NewReflex(200, 200, 50, 3, 60, 100, rand.New(rand.NewSource(seed)))
}

func TestReflexPrayerTurnsOnBelowSixtyPercent(t *testing.T) {
	r := newTestReflex(1)
	// 60% of 200 = 120. Deal damage to get HP to exactly 120 (60%).
	// Starting at 200, deal 80 damage over ticks (no death, no potion yet).
	a := r.Tick(80) // hp: 200 -> 120
	if !r.prayerActive {
		t.Errorf("prayer should be active at hp=%d (60%% of max)", a.HPAfter)
	}
	if a.Action != "prayer-on" {
		t.Errorf("action = %q, want prayer-on", a.Action)
	}
}

func TestReflexPrayerTurnsOffAboveEightyPercent(t *testing.T) {
	r := newTestReflex(1)
	// Get prayer on: damage to 60%.
	r.Tick(80) // hp 120, prayer on
	// Heal back above 80% (160/200 = 80%). Use a big potion heal.
	// Set potionCD low so the drink fires; but easier: just set HP directly.
	r.hp = 170 // above 80%
	a := r.Tick(0)
	if r.prayerActive {
		t.Error("prayer should be off at hp=170 (above 80%)")
	}
	if a.Action != "prayer-off" {
		t.Errorf("action = %q, want prayer-off", a.Action)
	}
}

func TestReflexDrinksPotionBelowFiftyPercent(t *testing.T) {
	r := newTestReflex(1)
	// 50% of 200 = 100. Deal damage to get below that.
	// potionCD starts at 0 and increments each tick; need >= 20 ticks.
	// Simulate 20 ticks of small damage to build up CD, then a big hit.
	for i := 0; i < 20; i++ {
		r.Tick(1) // tiny damage, builds potionCD
	}
	// Now HP should be around 180. Deal a big hit to go below 50%.
	a := r.Tick(90) // hp drops to ~90 (below 100)
	// The potion should have fired this tick (CD was >= 20, hp <= 100).
	if a.Action != "drink-potion" {
		t.Errorf("action = %q, want drink-potion (hp=%d)", a.Action, a.HPAfter)
	}
	if r.potions != 2 {
		t.Errorf("potions = %d, want 2 (started with 3)", r.potions)
	}
}

func TestReflexDiedAndRespawns(t *testing.T) {
	r := NewReflex(100, 50, 0, 0, 0, 30, rand.New(rand.NewSource(1)))
	// No prayer points, no potions. Deal 60 damage -> hp goes to -10 -> death.
	a := r.Tick(60)
	if a.Action != "respawn" {
		t.Errorf("action = %q, want respawn", a.Action)
	}
	if r.deaths != 1 {
		t.Errorf("deaths = %d, want 1", r.deaths)
	}
	if r.hp != 30 {
		t.Errorf("hp after respawn = %d, want 30 (respawnHP)", r.hp)
	}
}

func TestReflexPrayerDrainsOverTime(t *testing.T) {
	r := newTestReflex(1)
	pointsBefore := r.prayerPoints
	// Turn prayer on.
	r.Tick(80) // hp 120, prayer on
	pointsAfterOn := r.prayerPoints
	// Run 10 more ticks (1 second). Prayer should drain 1 point at tick%10==0.
	for i := 0; i < 10; i++ {
		r.Tick(0)
	}
	pointsAfter := r.prayerPoints
	if pointsAfter > pointsBefore-1 {
		t.Errorf("prayer points: before=%d after-on=%d after-10-ticks=%d, expected drain of ~1",
			pointsBefore, pointsAfterOn, pointsAfter)
	}
}

func TestReflexDeterministicForSeed(t *testing.T) {
	run := func(seed int64) []string {
		r := newTestReflex(seed)
		var actions []string
		for i := 0; i < 200; i++ {
			dmg := 0
			if i%5 == 0 {
				dmg = 15 // periodic hits
			}
			a := r.Tick(dmg)
			actions = append(actions, a.Action)
		}
		return actions
	}
	a, b := run(42), run(42)
	if len(a) != len(b) {
		t.Fatalf("length mismatch %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("seeded reflex diverged at tick %d: %s vs %s", i, a[i], b[i])
		}
	}
}

func TestReflexStateSnapshot(t *testing.T) {
	r := newTestReflex(1)
	s := r.State()
	if s.MaxHP != 200 || s.HP != 200 {
		t.Errorf("initial state: hp=%d maxHP=%d, want 200/200", s.HP, s.MaxHP)
	}
	if s.Potions != 3 {
		t.Errorf("potions = %d, want 3", s.Potions)
	}
	r.Tick(80) // triggers prayer
	s2 := r.State()
	if !s2.PrayerActive {
		t.Error("state should show prayer active after damage")
	}
}
