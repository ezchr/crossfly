//go:build crossfly_full

package javamob

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Sensing is Java's Sensing: line of sight results, kept for one tick.
type Sensing struct {
	seen, unseen map[*world.EntityHandle]struct{}
}

func (s *Sensing) tick() {
	clear(s.seen)
	clear(s.unseen)
}

// HasLineOfSight is Sensing.hasLineOfSight (cached for the tick).
func (s *Sensing) HasLineOfSight(m *Mob, e world.Entity) bool {
	h := e.H()
	if _, ok := s.seen[h]; ok {
		return true
	}
	if _, ok := s.unseen[h]; ok {
		return false
	}
	if s.seen == nil {
		s.seen, s.unseen = map[*world.EntityHandle]struct{}{}, map[*world.EntityHandle]struct{}{}
	}
	ok := m.HasLineOfSight(e)
	if ok {
		s.seen[h] = struct{}{}
	} else {
		s.unseen[h] = struct{}{}
	}
	return ok
}

// HasLineOfSight is LivingEntity.hasLineOfSight: no block collision box between the eyes (within 128 blocks).
func (m *Mob) HasLineOfSight(e world.Entity) bool {
	from, to := m.EyePosition(), eyeOf(e)
	if to.Sub(from).LenSqr() > 128*128 {
		return false
	}
	return Clear(m.Tx(), from, to)
}

// Clear reports whether no block collision box lies on the line from start to end (a COLLIDER clip that misses).
func Clear(tx *world.Tx, start, end mgl64.Vec3) bool {
	clear := true
	trace.TraverseBlocks(start, end, func(pos cube.Pos) bool {
		if pos.OutOfBounds(tx.Range()) {
			return true
		}
		b := tx.Block(pos)
		if trace.BlockIntersects(pos, tx, b, start, end) {
			clear = false
			return false
		}
		return true
	})
	return clear
}

// TargetingConditions are Java's TargetingConditions.
type TargetingConditions struct {
	// Combat is forCombat (the targeter must be able to attack the target); false is forNonCombat.
	Combat bool
	// Range is range(r); 0 or less means any distance.
	Range float64
	// IgnoreLineOfSight is ignoreLineOfSight, IgnoreInvisibility ignoreInvisibilityTesting.
	IgnoreLineOfSight, IgnoreInvisibility bool
	// Selector is the extra test, or nil.
	Selector func(e world.Entity, tx *world.Tx) bool
}

// ForCombat returns TargetingConditions.forCombat().range(r).
func ForCombat(r float64) TargetingConditions { return TargetingConditions{Combat: true, Range: r} }

// ForNonCombat returns TargetingConditions.forNonCombat().range(r).
func ForNonCombat(r float64) TargetingConditions { return TargetingConditions{Range: r} }

// Test is TargetingConditions.test with m as the targeter.
func (c TargetingConditions) Test(m *Mob, target world.Entity) bool {
	if target == nil || target.H() == m.H() || !seenByAnyone(target) {
		return false
	}
	tx := m.Tx()
	if c.Selector != nil && !c.Selector(target, tx) {
		return false
	}
	if c.Combat && !m.validTarget(target) {
		return false
	}
	if c.Range > 0 {
		mod := 1.0
		if !c.IgnoreInvisibility {
			mod = VisibilityPercent(target)
		}
		vis := math.Max(c.Range*mod, 2)
		if target.Position().Sub(m.Position()).LenSqr() > vis*vis {
			return false
		}
	}
	if !c.IgnoreLineOfSight && !m.s.sensing.HasLineOfSight(m, target) {
		return false
	}
	return true
}

// seenByAnyone is LivingEntity.canBeSeenByAnyone: alive and not a spectator.
func seenByAnyone(e world.Entity) bool {
	if l, ok := e.(entity.Living); ok && l.Dead() {
		return false
	}
	if p, ok := e.(*player.Player); ok && !p.GameMode().Visible() {
		return false
	}
	return true
}

// VisibilityPercent is LivingEntity.getVisibilityPercent: 0.8 sneaking, 0.7 times the armour cover while invisible.
func VisibilityPercent(e world.Entity) float64 {
	v := 1.0
	if s, ok := e.(interface{ Sneaking() bool }); ok && s.Sneaking() {
		v *= 0.8
	}
	if i, ok := e.(interface{ Invisible() bool }); ok && i.Invisible() {
		cover := float32(0)
		if a, ok := e.(interface{ Armour() *inventory.Armour }); ok && a.Armour() != nil {
			n := 0
			for _, it := range a.Armour().Items() {
				if !it.Empty() {
					n++
				}
			}
			cover = float32(n) / 4
		}
		cover = max(cover, 0.1)
		v *= 0.7 * float64(cover)
	}
	return math.Max(0, math.Min(10, v))
}

// NearestPlayer is ServerLevel.getNearestPlayer(conditions, mob, x, eyeY, z): the closest player that passes c.
func (m *Mob) NearestPlayer(c TargetingConditions) (*player.Player, bool) {
	p := m.Position()
	from := mgl64.Vec3{p[0], m.EyeY(), p[2]}
	var best *player.Player
	bestD := -1.0
	for e := range m.Tx().Players() {
		pl, ok := e.(*player.Player)
		if !ok || !c.Test(m, pl) {
			continue
		}
		d := pl.Position().Sub(from).LenSqr()
		if bestD < 0 || d < bestD {
			best, bestD = pl, d
		}
	}
	return best, best != nil
}

// NearestEntity is ServerLevel.getNearestEntity over the entities in the box that match: the closest to the mob's
// eyes that passes c.
func (m *Mob) NearestEntity(box cube.BBox, match func(world.Entity) bool, c TargetingConditions) (world.Entity, bool) {
	p := m.Position()
	from := mgl64.Vec3{p[0], m.EyeY(), p[2]}
	var best world.Entity
	bestD := -1.0
	for e := range m.Tx().EntitiesWithin(box) {
		if e.H() == m.H() || !match(e) || !overlaps(boxOf(e), box) || !c.Test(m, e) {
			continue
		}
		d := e.Position().Sub(from).LenSqr()
		if bestD < 0 || d < bestD {
			best, bestD = e, d
		}
	}
	return best, best != nil
}

// IsType reports whether e is an entity of the identifier ("minecraft:villager"), Java or Bedrock engine.
func IsType(e world.Entity, id string) bool { return e.H().Type().EncodeEntity() == id }

// IsPlayer reports whether e is a player.
func IsPlayer(e world.Entity) bool { _, ok := e.(*player.Player); return ok }
