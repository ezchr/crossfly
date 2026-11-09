//go:build crossfly_full

package social

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// dropItemFor is minecraft:behavior.drop_item_for (BDS DropItemForGoal, the
// cat's morning gift): within time_of_day_range, the mob walks to the
// nearest entity matching entity_types, and once within offering_distance
// fires on_drop_attempt and, with drop_item_chance, drops an item of
// loot_table. Out of reach it teleports next to the target (teleport_offset)
// when farther than minimum_teleport_distance and no path is found.
//
//	cooldown                   0.2 s between attempts
//	drop_item_chance           1.0
//	offering_distance          1.0
//	minimum_teleport_distance  2.0 (BDS +200)
//	max_head_look_at_height    10.0 (BDS +204)
//	speed_multiplier           1.0
//	seconds_before_pickup      0
//	teleport_offset            [0, 1, 0]
//	time_of_day_range          [0, 1] (celestial angle; 0.75 is sunrise)
//	entity_types               mob descriptors (max_dist 16)
//
// search_range/search_height/goal_radius/target_range (a free block to
// stand on near the target) are approximated by walking to the target.
// Control flags move|look.
type dropItemFor struct {
	mob.GoalBase
	cooldown      int
	chance        float64
	offer         float64
	teleportMin   float64
	lookHeight    float64
	speed         float64
	pickupDelay   time.Duration
	teleportOff   mgl64.Vec3
	todMin, todMx float64
	types         []descriptor
	loot          string
	onAttempt     kit.Trigger

	next   int64
	target *world.EntityHandle
	done   bool
	repath int
}

// descriptor is a minimal BDS MobDescriptor: filters with the candidate as
// "other", and max_dist.
type descriptor struct {
	filter  mob.Filter
	maxDist float64
}

func parseDescriptors(raw json.RawMessage, def float64) ([]descriptor, error) {
	if kit.IsNull(raw) {
		return nil, nil
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		list = []json.RawMessage{raw}
	}
	var out []descriptor
	for _, r := range list {
		var v struct {
			Filters json.RawMessage `json:"filters"`
			MaxDist *float64        `json:"max_dist"`
		}
		if err := json.Unmarshal(r, &v); err != nil {
			return nil, err
		}
		f, err := kit.Filter(v.Filters)
		if err != nil {
			return nil, err
		}
		out = append(out, descriptor{filter: f, maxDist: kit.Or(v.MaxDist, def)})
	}
	return out, nil
}

// match returns the first descriptor e matches.
func match(m *mob.Mob, ds []descriptor, e world.Entity) bool {
	d := e.Position().Sub(m.Position()).Len()
	for _, x := range ds {
		if d <= x.maxDist && x.filter.Test(kit.OtherCtx(m, e)) {
			return true
		}
	}
	return false
}

func maxDist(ds []descriptor, def float64) float64 {
	r := 0.0
	for _, d := range ds {
		r = max(r, d.maxDist)
	}
	if r == 0 {
		return def
	}
	return r
}

func newDropItemFor(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Cooldown     *float64        `json:"cooldown"`
		Chance       *float64        `json:"drop_item_chance"`
		Offering     *float64        `json:"offering_distance"`
		TeleportMin  *float64        `json:"minimum_teleport_distance"`
		LookHeight   *float64        `json:"max_head_look_at_height"`
		Speed        *float64        `json:"speed_multiplier"`
		PickupDelay  float64         `json:"seconds_before_pickup"`
		TeleportOff  []float64       `json:"teleport_offset"`
		TimeOfDay    json.RawMessage `json:"time_of_day_range"`
		EntityTypes  json.RawMessage `json:"entity_types"`
		LootTable    string          `json:"loot_table"`
		OnDropAttemp json.RawMessage `json:"on_drop_attempt"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	g := &dropItemFor{
		GoalBase:    mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)},
		cooldown:    kit.Ticks(kit.Or(v.Cooldown, 0.2)),
		chance:      kit.Or(v.Chance, 1),
		offer:       kit.Or(v.Offering, 1),
		teleportMin: kit.Or(v.TeleportMin, 2),
		lookHeight:  kit.Or(v.LookHeight, 10),
		speed:       kit.Or(v.Speed, 1),
		pickupDelay: time.Duration(v.PickupDelay * float64(time.Second)),
		teleportOff: mgl64.Vec3{0, 1, 0},
		loot:        v.LootTable,
	}
	if len(v.TeleportOff) == 3 {
		g.teleportOff = mgl64.Vec3{v.TeleportOff[0], v.TeleportOff[1], v.TeleportOff[2]}
	}
	r, err := kit.ParseRange(v.TimeOfDay, kit.Range{Min: 0, Max: 1})
	if err != nil {
		return nil, fmt.Errorf("%v: time_of_day_range: %w", spec.Key, err)
	}
	g.todMin, g.todMx = r.Min, r.Max
	if g.types, err = parseDescriptors(v.EntityTypes, 16); err != nil {
		return nil, fmt.Errorf("%v: entity_types: %w", spec.Key, err)
	}
	if g.onAttempt, err = kit.ParseTrigger(v.OnDropAttemp); err != nil {
		return nil, err
	}
	return g, nil
}

func (g *dropItemFor) inTime(m *mob.Mob) bool {
	t := timeOfDay(m.Tx())
	return t >= g.todMin && t <= g.todMx
}

// CanUse ...
func (g *dropItemFor) CanUse(m *mob.Mob) bool {
	if m.Ticks() < g.next || !g.inTime(m) {
		return false
	}
	// The owner first, when it matches.
	if o, ok := entityOf(m, m.Owner()); ok && kit.Alive(o) && match(m, g.types, o) {
		g.target = o.H()
		return true
	}
	r := maxDist(g.types, 16)
	e, ok := kit.Nearest(m, r, r, func(e world.Entity) bool { return match(m, g.types, e) })
	if !ok {
		return false
	}
	g.target = e.H()
	return true
}

// CanContinueToUse ...
func (g *dropItemFor) CanContinueToUse(m *mob.Mob) bool {
	if g.done || !g.inTime(m) {
		return false
	}
	t, ok := entityOf(m, g.target)
	return ok && kit.Alive(t)
}

// Start ...
func (g *dropItemFor) Start(m *mob.Mob) {
	g.done, g.repath = false, 0
}

// Stop ...
func (g *dropItemFor) Stop(m *mob.Mob) {
	kit.Stop(m)
	g.target = nil
	g.next = m.Ticks() + int64(g.cooldown)
}

// Tick ...
func (g *dropItemFor) Tick(m *mob.Mob) {
	t, ok := entityOf(m, g.target)
	if !ok {
		return
	}
	if t.Position()[1]-m.Position()[1] <= g.lookHeight {
		kit.LookAtEntity(m, t)
	}
	d := t.Position().Sub(m.Position()).Len()
	if d <= g.offer {
		g.attempt(m, t)
		return
	}
	if g.repath--; g.repath > 0 {
		return
	}
	g.repath = 10
	if !kit.MoveTo(m, t.Position(), g.speed) && d > g.teleportMin {
		m.Teleport(t.Position().Add(g.teleportOff))
	}
}

// attempt fires on_drop_attempt and maybe drops the gift.
func (g *dropItemFor) attempt(m *mob.Mob, t world.Entity) {
	g.done = true
	kit.Stop(m)
	if m.Rand().Float64() < g.chance && g.loot != "" {
		if lt, err := core.LoadLootTable(g.loot); err == nil && lt != nil {
			for _, s := range lt.Roll(&core.LootContext{Mob: m}) {
				opts := world.EntitySpawnOpts{Position: m.Position().Add(mgl64.Vec3{0, 0.5, 0}), Velocity: mgl64.Vec3{0, 0.2, 0}}
				m.Tx().AddEntity(entity.NewItemPickupDelay(opts, s, g.pickupDelay))
			}
		}
	}
	ctx := kit.OtherCtx(m, t)
	g.onAttempt.Fire(m, ctx)
}

// AppendDebugInfo ...
func (g *dropItemFor) AppendDebugInfo(b *strings.Builder) { b.WriteString("DropItemFor") }
