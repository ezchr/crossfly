//go:build crossfly_full

package village

import (
	"encoding/json"
	"fmt"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Admire is minecraft:admire_item (BDS AdmireItemComponent): a piglin that
// picks up an item its shareables mark "admire" holds it for duration
// seconds, unless it was hurt by a mob within cooldown_after_being_attacked
// seconds.
type Admire struct {
	duration, cooldown int // ticks
	Admiring           bool
	Item               item.Stack
	Owner              *world.EntityHandle
	End                int64
}

func newAdmire(_ string, raw json.RawMessage) (mob.Component, error) {
	j := struct {
		Duration *int `json:"duration"`
		Cooldown *int `json:"cooldown_after_being_attacked"`
	}{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &j); err != nil {
			return nil, fmt.Errorf("admire_item: %w", err)
		}
	}
	a := &Admire{duration: 200, cooldown: 200}
	if j.Duration != nil {
		a.duration = *j.Duration * 20
	}
	if j.Cooldown != nil {
		a.cooldown = *j.Cooldown * 20
	}
	return a, nil
}

func (*Admire) Attach(*mob.Mob) {}
func (*Admire) Detach(*mob.Mob) {}

// AdmireOf returns m's admire_item component.
func AdmireOf(m *mob.Mob) (*Admire, bool) { return mob.ComponentOf[*Admire](m) }

// HurtByMobWithin reports whether a mob hurt m within the last ticks.
func HurtByMobWithin(m *mob.Mob, ticks int) bool {
	e, t, ok := m.LastHurtBy()
	if !ok || e == nil {
		return false
	}
	if _, isMob := mob.Of(e); !isMob {
		return false
	}
	return m.Ticks()-t <= int64(ticks)
}

// Cooldown is cooldown_after_being_attacked in ticks.
func (a *Admire) Cooldown() int { return a.cooldown }

// PickedUp is AdmireItemComponent::onAdmireItemPickedUp.
func (a *Admire) PickedUp(m *mob.Mob, s item.Stack, owner world.Entity) {
	if HurtByMobWithin(m, a.cooldown) {
		return
	}
	a.Admiring, a.Item, a.End = true, s, m.Ticks()+int64(a.duration)
	a.Owner = nil
	if owner != nil {
		a.Owner = owner.H()
	}
}

// Stop is AdmireItemComponent::stopAdmiring.
func (a *Admire) Stop() { a.Admiring, a.Item, a.End, a.Owner = false, item.Stack{}, 0, nil }

// Barter is minecraft:barter (BDS BarterComponent): a piglin that picks up
// an item its shareables mark "barter" wants to barter it.
type Barter struct {
	Table    string
	cooldown int
	Wants    bool
	Thrower  *world.EntityHandle
}

func newBarter(_ string, raw json.RawMessage) (mob.Component, error) {
	j := struct {
		Table    string `json:"barter_table"`
		Cooldown *int   `json:"cooldown_after_being_attacked"`
	}{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &j); err != nil {
			return nil, fmt.Errorf("barter: %w", err)
		}
	}
	b := &Barter{Table: j.Table, cooldown: 200}
	if j.Cooldown != nil {
		b.cooldown = *j.Cooldown * 20
	}
	return b, nil
}

func (*Barter) Attach(*mob.Mob) {}
func (*Barter) Detach(*mob.Mob) {}

// BarterOf returns m's barter component.
func BarterOf(m *mob.Mob) (*Barter, bool) { return mob.ComponentOf[*Barter](m) }

// Cooldown is cooldown_after_being_attacked in ticks.
func (b *Barter) Cooldown() int { return b.cooldown }

// PickedUp is BarterComponent::onBarterItemPickedUp.
func (b *Barter) PickedUp(m *mob.Mob, thrower world.Entity) {
	if HurtByMobWithin(m, b.cooldown) {
		return
	}
	b.Wants = true
	b.Thrower = nil
	if thrower != nil {
		b.Thrower = thrower.H()
	}
}

// Stop is BarterComponent::stopBarter.
func (b *Barter) Stop() { b.Wants = false }

// Roll rolls the barter table.
func (b *Barter) Roll(m *mob.Mob) []item.Stack {
	t, err := core.LoadLootTable(b.Table)
	if err != nil || t == nil {
		return nil
	}
	return t.Roll(&core.LootContext{Mob: m})
}

// celebrateHunt is minecraft:celebrate_hunt (BDS CelebrateHuntComponent):
// killing a celebration target makes the mob (and, with broadcast, every
// mob of its type within radius) celebrate for duration seconds (10%: the
// special dance), playing celebrate_sound every sound_interval, until it
// gets a target.
type celebrateHunt struct {
	broadcast bool
	radius    float64
	duration  int
	targets   mob.Filter
	sound     string
	interval  [2]float64

	celebrating bool
	special     bool
	end         int64
	nextSound   int64
}

func newCelebrateHunt(_ string, raw json.RawMessage) (mob.Component, error) {
	j := struct {
		Broadcast *bool           `json:"broadcast"`
		Radius    *float64        `json:"radius"`
		Duration  *int            `json:"duration"`
		Targets   json.RawMessage `json:"celebration_targets"`
		Sound     string          `json:"celebrate_sound"`
		Interval  *FloatRange     `json:"sound_interval"`
	}{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &j); err != nil {
			return nil, fmt.Errorf("celebrate_hunt: %w", err)
		}
	}
	c := &celebrateHunt{broadcast: true, radius: 16, duration: 4, sound: j.Sound}
	if j.Broadcast != nil {
		c.broadcast = *j.Broadcast
	}
	if j.Radius != nil {
		c.radius = *j.Radius
	}
	if j.Duration != nil {
		c.duration = *j.Duration
	}
	if j.Interval != nil {
		c.interval = [2]float64{j.Interval.Min, j.Interval.Max}
	}
	f, err := mob.CompileFilter(j.Targets)
	if err != nil {
		return nil, err
	}
	c.targets = f
	return c, nil
}

func (*celebrateHunt) Attach(*mob.Mob) {}
func (c *celebrateHunt) Detach(m *mob.Mob) {
	if c.celebrating {
		c.stop(m)
	}
}

func (c *celebrateHunt) start(m *mob.Mob, special bool) {
	c.celebrating, c.special, c.end = true, special, m.Ticks()+int64(c.duration*20)
	SetCelebrating(m, true, special)
}

func (c *celebrateHunt) stop(m *mob.Mob) {
	c.celebrating = false
	SetCelebrating(m, false, false)
	SetCelebrating(m, false, true)
}

// Tick is CelebrateHuntSystem.
func (c *celebrateHunt) Tick(m *mob.Mob) {
	if !c.celebrating {
		return
	}
	if m.Ticks() > c.end {
		c.stop(m)
		return
	}
	if _, ok := m.Target(); ok {
		c.stop(m)
		return
	}
	if c.nextSound < m.Ticks() {
		m.PlaySound(c.sound)
		c.nextSound = m.Ticks() + int64(randRange(m, c.interval)*20)
	}
}

// killed is Actor::killed for celebrate_hunt.
func (c *celebrateHunt) killed(m *mob.Mob, victim world.Entity) {
	// The targets filter tests the victim as its subject (Actor::killed
	// evaluates it on the killed actor: celebration_targets uses the
	// default subject).
	ctx := m.Ctx()
	ctx.Other = victim
	if vm, ok := mob.Of(victim); ok {
		ctx.Self = vm
	}
	if c.targets == nil || !c.targets.Test(ctx) {
		return
	}
	special := m.Rand().IntN(10) == 1
	c.start(m, special)
	if !c.broadcast {
		return
	}
	box := m.BBox().Grow(c.radius)
	for e := range m.Tx().EntitiesWithin(box) {
		o, ok := mob.Of(e)
		if !ok || o.H() == m.H() || o.Identifier() != m.Identifier() {
			continue
		}
		if oc, ok := mob.ComponentOf[*celebrateHunt](o); ok {
			oc.start(o, special)
		}
	}
}

// FloatRange is a {min,max} / {range_min,range_max} / [a,b] / number range.
type FloatRange struct{ Min, Max float64 }

func (r *FloatRange) UnmarshalJSON(b []byte) error {
	var f float64
	if json.Unmarshal(b, &f) == nil {
		r.Min, r.Max = f, f
		return nil
	}
	var a []float64
	if json.Unmarshal(b, &a) == nil {
		if len(a) > 0 {
			r.Min, r.Max = a[0], a[len(a)-1]
		}
		return nil
	}
	var o struct {
		Min      *float64 `json:"min"`
		Max      *float64 `json:"max"`
		RangeMin *float64 `json:"range_min"`
		RangeMax *float64 `json:"range_max"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return err
	}
	if o.Min != nil {
		r.Min = *o.Min
	}
	if o.RangeMin != nil {
		r.Min = *o.RangeMin
	}
	r.Max = r.Min
	if o.Max != nil {
		r.Max = *o.Max
	}
	if o.RangeMax != nil {
		r.Max = *o.RangeMax
	}
	return nil
}

func randRange(m *mob.Mob, r [2]float64) float64 {
	if r[1] <= r[0] {
		return r[0]
	}
	return r[0] + m.Rand().Float64()*(r[1]-r[0])
}

func init() {
	mob.RegisterHurtHook(mob.HurtHook{Name: "village/celebrate_hunt", Hurt: func(victim *mob.Mob, _ float64, _ world.DamageSource) {
		if !victim.Dead() && victim.Health() > 0 {
			return
		}
		killer, _, ok := victim.LastHurtBy()
		if !ok {
			return
		}
		km, ok := mob.Of(killer)
		if !ok {
			return
		}
		if c, ok := mob.ComponentOf[*celebrateHunt](km); ok {
			c.killed(km, victim)
		}
	}})
}

// ThrowItems drops stacks from 1 block above m with a small random push
// (BarterGoal::_getThrowVector).
func ThrowItems(m *mob.Mob, stacks []item.Stack) {
	for _, s := range stacks {
		if s.Empty() {
			continue
		}
		v := mgl64.Vec3{m.Rand().Float64()*0.4 - 0.2, 0.3, m.Rand().Float64()*0.4 - 0.2}
		dropItem(m.Tx(), m.Position().Add(mgl64.Vec3{0, 1}), v, s)
	}
}

// IsAdmiring reports whether the mob admires an item (pickup_items stops).
func (a *Admire) IsAdmiring() bool { return a.Admiring }
