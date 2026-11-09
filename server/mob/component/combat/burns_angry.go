//go:build crossfly_full

package combat

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/world"
)

// BurnsInDaylight is minecraft:burns_in_daylight (BDS BurnsInDaylightSystem,
// Java Mob.isSunBurnTick): in the day, at brightness above 0.5, with a
// (brightness-0.4)*2/30 chance per tick and the sky visible from the eyes,
// a mob out of water and rain catches fire for 8 seconds, or its helmet
// takes 0-1 durability damage instead.
type BurnsInDaylight struct{}

func newBurnsInDaylight(string, json.RawMessage) (mob.Component, error) {
	return &BurnsInDaylight{}, nil
}

// Attach ...
func (*BurnsInDaylight) Attach(*mob.Mob) {}

// Detach ...
func (*BurnsInDaylight) Detach(*mob.Mob) {}

// SunBurnTick reports whether the sun burns m this tick (consumes a roll).
func SunBurnTick(m *mob.Mob) bool {
	tx := m.Tx()
	if !sense.IsDay(tx) {
		return false
	}
	pos := m.Pos()
	br := sense.Brightness(tx, cube.PosFromVec3(pos))
	if br <= 0.5 || m.Rand().Float64()*30 >= (br-0.4)*2 {
		return false
	}
	if sense.InWaterOrRain(tx, m) {
		return false
	}
	eye := cube.Pos{int(math.Floor(pos[0])), int(math.Floor(m.EyePosition()[1])), int(math.Floor(pos[2]))}
	return sense.SkyVisible(tx, eye)
}

func burnTick(m *mob.Mob) {
	if _, ok := mob.ComponentOf[*BurnsInDaylight](m); !ok || !SunBurnTick(m) {
		return
	}
	a := m.Armour()
	if h := a.Helmet(); !h.Empty() {
		if h.MaxDurability() > 0 {
			h = h.Damage(m.Rand().IntN(2))
			a.SetHelmet(h)
		}
		return
	}
	m.SetOnFire(8 * time.Second)
}

// Angry is minecraft:angry (BDS AngryComponent/AngrySystem): while the mob
// has a target that passes "filters" it is angry (Meta.Angry) for duration
// ± duration_delta seconds, restarted by every attack; then calm_event fires
// and the target is dropped. It can alert mobs around (broadcast_*).
type Angry struct {
	duration, delta     int
	broadcast           bool
	broadcastOnAttack   bool
	broadcastOnAttacked bool
	broadcastRange      float64
	broadcastTargets    []string
	broadcastFilter     mob.Filter
	filter              mob.Filter
	calm                sense.Trigger

	timer    int
	attacked bool
	started  bool
	calmed   bool
}

func newAngry(_ string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		Duration            *float64        `json:"duration"`
		DurationDelta       float64         `json:"duration_delta"`
		BroadcastAnger      bool            `json:"broadcast_anger"`
		BroadcastOnAttack   bool            `json:"broadcast_anger_on_attack"`
		BroadcastOnAttacked bool            `json:"broadcast_anger_on_being_attacked"`
		BroadcastRange      *float64        `json:"broadcast_range"`
		BroadcastTargets    []string        `json:"broadcast_targets"`
		BroadcastFilters    json.RawMessage `json:"broadcast_filters"`
		Filters             json.RawMessage `json:"filters"`
		CalmEvent           json.RawMessage `json:"calm_event"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("minecraft:angry: %w", err)
	}
	a := &Angry{duration: 25, delta: int(v.DurationDelta), broadcast: v.BroadcastAnger, broadcastOnAttack: v.BroadcastOnAttack,
		broadcastOnAttacked: v.BroadcastOnAttacked, broadcastRange: 20, broadcastTargets: v.BroadcastTargets}
	if v.Duration != nil {
		a.duration = int(*v.Duration)
	}
	if v.BroadcastRange != nil {
		a.broadcastRange = *v.BroadcastRange
	}
	var err error
	if a.broadcastFilter, err = mob.CompileFilter(v.BroadcastFilters); err != nil {
		return nil, err
	}
	if a.filter, err = mob.CompileFilter(v.Filters); err != nil {
		return nil, err
	}
	if a.calm, err = sense.ParseTrigger(v.CalmEvent); err != nil {
		return nil, err
	}
	return a, nil
}

// Attach starts the timer.
func (a *Angry) Attach(m *mob.Mob) { a.RestartAngerTimer(m); a.attacked = false }

// Detach calms the mob.
func (a *Angry) Detach(m *mob.Mob) { m.UpdateMeta(func(meta *mob.Meta) { meta.Angry = false }) }

// RestartAngerTimer is AngryComponent::restartTimer: 20 x (duration ±
// delta) ticks; a negative duration never runs out.
func (a *Angry) RestartAngerTimer(m *mob.Mob) {
	if a.duration < 0 {
		a.timer = -1
		return
	}
	d := a.duration
	if a.delta > 0 {
		d += m.Rand().IntN(2*a.delta+1) - a.delta
	}
	a.timer = 20 * d
	if a.started {
		a.attacked = true
	}
	a.started = true
}

// Remaining is the anger left in ticks (-1: forever).
func (a *Angry) Remaining() int { return a.timer }

func (a *Angry) canGetAngry(m *mob.Mob) (world.Entity, bool) {
	t, ok := m.Target()
	if !ok || !sense.Alive(t) {
		return nil, false
	}
	ctx := m.Ctx()
	ctx.Other, ctx.Target = t, t
	return t, a.filter.Test(ctx)
}

func (a *Angry) tick(m *mob.Mob) {
	t, ok := a.canGetAngry(m)
	if !ok {
		a.calmDown(m)
		return
	}
	a.calmed = false
	m.UpdateMeta(func(meta *mob.Meta) { meta.Angry = true })
	_, hurtTick, hurt := m.LastHurtBy()
	if a.broadcast || (a.broadcastOnAttack && a.attacked) || (a.broadcastOnAttacked && hurt && hurtTick > m.Ticks()-2) {
		a.alertFriends(m, t)
	}
	a.attacked = false
	if a.timer >= 0 {
		if a.timer--; a.timer <= 0 {
			a.calmDown(m)
		}
	}
}

func (a *Angry) calmDown(m *mob.Mob) {
	if a.calmed {
		return
	}
	a.calmed = true
	m.UpdateMeta(func(meta *mob.Meta) { meta.Angry = false })
	m.ResetTarget()
	a.calm.Run(m, nil)
}

// alertFriends makes the mobs within broadcast_range (of the
// broadcast_targets families, or the same type) target t.
func (a *Angry) alertFriends(m *mob.Mob, t world.Entity) {
	for e := range m.Tx().EntitiesWithin(m.BBox().Grow(a.broadcastRange)) {
		o, ok := mob.Of(e)
		if !ok || o.H() == m.H() || o.H() == t.H() || o.Dead() {
			continue
		}
		if len(a.broadcastTargets) > 0 {
			match := false
			for _, f := range a.broadcastTargets {
				if o.HasFamily(f) {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		} else if o.Identifier() != m.Identifier() {
			continue
		}
		ctx := m.Ctx()
		ctx.Other = o
		if !a.broadcastFilter.Test(ctx) {
			continue
		}
		if cur, has := o.Target(); has && cur.H() == t.H() {
			continue
		}
		o.SetTarget(t)
	}
}

// EncodeNBT ...
func (a *Angry) EncodeNBT() map[string]any { return map[string]any{"Timer": int32(a.timer)} }

// DecodeNBT ...
func (a *Angry) DecodeNBT(_ *mob.Mob, d map[string]any) {
	if v, ok := d["Timer"].(int32); ok {
		a.timer = int(v)
	}
}
