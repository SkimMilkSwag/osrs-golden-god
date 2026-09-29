package main

import (
	"fmt"
	"math/rand"
)

// Reflex is the fast rule-based layer that sits under the LLM planner. The
// planner sets intent ("kill this goblin"); the reflexes keep the bot alive
// on a per-tick clock without round-tripping to a model. In the MVP plan this
// is the part that gets routed through rules (or a cheap local model) while
// the planning model stays on the slow 5–15s orient–decide–act loop.
//
// The reflex layer here is deliberately deterministic given an RNG seed so
// the death/prayer/potion logic can be pinned in tests — no client, no clock.
type Reflex struct {
	rng *rand.Rand

	// HP / max HP of the bot character.
	hp    int
	maxHP int

	// Prayer state.
	prayerActive bool
	prayerPoints int // points remaining; each second of prayer drains 1 point

	// Potions.
	potions    int // potions in the inventory
	potionHeal int // flat heal per potion drink (before variance)
	potionCD   int // ticks since last drink; a drink sets this to 0

	// Death / respawn.
	deaths    int
	respawnHP int // HP restored on respawn (typically a fraction of max)

	// Tick counter for the session the reflex is running in.
	tick int
}

// NewReflex builds a reflex layer with the given starting state.
func NewReflex(maxHP, hp, prayerPoints, potions, potionHeal, respawnHP int, rng *rand.Rand) *Reflex {
	return &Reflex{
		rng:          rng,
		hp:           hp,
		maxHP:        maxHP,
		prayerPoints: prayerPoints,
		potions:      potions,
		potionHeal:   potionHeal,
		respawnHP:    respawnHP,
	}
}

// ReflexAction is the decision a reflex made this tick.
type ReflexAction struct {
	Tick        int
	Action      string // "prayer-on", "prayer-off", "drink-potion", "respawn", "none"
	Detail      string
	HPAfter     int
	PotionsLeft int
}

// Tick advances the reflex by one OSRS tick (600ms). The bot takes damage
// `damage` this tick (may be 0 if it's between hits). The reflex layer decides,
// in order: prayer, potion, death/respawn. It returns the action taken so a
// caller (or the ledger) can log it.
func (r *Reflex) Tick(damage int) ReflexAction {
	r.tick++

	// Apply incoming damage.
	if damage > 0 {
		r.hp -= damage
	}

	action := "none"
	detail := ""

	// --- Prayer: turn on when HP drops below 60% of max and we have points;
	// turn off when HP recovers above 80% (we're safe, stop burning points).
	if r.prayerPoints > 0 && !r.prayerActive && r.hp <= r.maxHP*6/10 {
		r.prayerActive = true
		action = "prayer-on"
		detail = fmt.Sprintf("hp=%d/%d", r.hp, r.maxHP)
	} else if r.prayerActive && r.hp >= r.maxHP*8/10 {
		r.prayerActive = false
		action = "prayer-off"
		detail = fmt.Sprintf("hp=%d/%d", r.hp, r.maxHP)
	}

	// Prayer drains 1 point per second (every 10 ticks at 600ms/tick).
	if r.prayerActive && r.tick%10 == 0 && r.prayerPoints > 0 {
		r.prayerPoints--
		if r.prayerPoints == 0 {
			r.prayerActive = false
		}
	}

	// --- Potion: drink when HP is below 50% and we have potions and the CD
	// has cleared. The potion has ±20% heal variance (Gaussian-ish: uniform
	// over [0.8, 1.2] * potionHeal) — real potions aren't perfectly flat.
	r.potionCD++
	if r.potions > 0 && r.hp > 0 && r.hp <= r.maxHP/2 && r.potionCD >= 20 {
		variance := 0.8 + 0.4*r.rng.Float64() // [0.8, 1.2)
		heal := int(float64(r.potionHeal) * variance)
		r.hp += heal
		if r.hp > r.maxHP {
			r.hp = r.maxHP
		}
		r.potions--
		r.potionCD = 0
		action = "drink-potion"
		detail = fmt.Sprintf("heal=%d hp=%d/%d potions_left=%d", heal, r.hp, r.maxHP, r.potions)
	}

	// --- Death: if HP hits 0 or below, respawn.
	if r.hp <= 0 {
		r.deaths++
		r.hp = r.respawnHP
		action = "respawn"
		detail = fmt.Sprintf("deaths=%d respawn_hp=%d", r.deaths, r.respawnHP)
	}

	return ReflexAction{
		Tick:        r.tick,
		Action:      action,
		Detail:      detail,
		HPAfter:     r.hp,
		PotionsLeft: r.potions,
	}
}

// State is a snapshot of the reflex's current state (for logging / ledger).
func (r *Reflex) State() ReflexState {
	return ReflexState{
		Tick:         r.tick,
		HP:           r.hp,
		MaxHP:        r.maxHP,
		PrayerActive: r.prayerActive,
		PrayerPoints: r.prayerPoints,
		Potions:      r.potions,
		Deaths:       r.deaths,
	}
}

// ReflexState is an immutable snapshot of the reflex at some point in time.
type ReflexState struct {
	Tick         int
	HP           int
	MaxHP        int
	PrayerActive bool
	PrayerPoints int
	Potions      int
	Deaths       int
}
