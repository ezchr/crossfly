//go:build crossfly_full

package javamob

import (
	"math"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// maxEntityCramming is the maxEntityCramming game rule (24), maxEntityCollisions Paper's
// collisions.max-entity-collisions default (8).
const (
	maxEntityCramming   = 24
	maxEntityCollisions = 8
)

// CrammingDamageSource is DamageSources.cramming: too many entities in one place.
type CrammingDamageSource struct{}

func (CrammingDamageSource) ReducedByArmour() bool     { return false }
func (CrammingDamageSource) ReducedByResistance() bool { return true }
func (CrammingDamageSource) Fire() bool                { return false }
func (CrammingDamageSource) IgnoreTotem() bool         { return false }

// pushEntities is LivingEntity.pushEntities with Paper's limit of 8 pushes per entity: the mob and the mobs and
// players its box touches push each other apart, and more than 23 of them hurt it (cramming).
func (m *Mob) pushEntities() {
	if !m.pushable() {
		return
	}
	tx := m.Tx()
	bb := m.BBox()
	var others []world.Entity
	for e := range tx.EntitiesWithin(bb) {
		if e.H() == m.H() || !pushableEntity(e) || !overlaps(boxOf(e), bb) {
			continue
		}
		others = append(others, e)
	}
	if len(others) == 0 {
		return
	}
	if len(others) > maxEntityCramming-1 && m.Rand().IntN(4) == 0 {
		count := 0
		for _, e := range others {
			if r, ok := e.(interface{ Riding() bool }); !ok || !r.Riding() {
				count++
			}
		}
		if count > maxEntityCramming-1 {
			m.Hurt(6, CrammingDamageSource{})
			if m.gone() || m.Dead() {
				return
			}
		}
	}
	s := m.s
	s.numCollisions = max(0, s.numCollisions-maxEntityCollisions)
	for _, e := range others {
		if s.numCollisions >= maxEntityCollisions {
			break
		}
		if o, ok := Of(e); ok {
			o.s.numCollisions++
		}
		s.numCollisions++
		pushApart(e, m)
	}
}

// pushable is LivingEntity.isPushable: alive and not on a ladder.
func (m *Mob) pushable() bool { return !m.Dead() && !m.OnClimbable() }

// pushableEntity is EntitySelector.pushableBy for the entities Crossfly has: mobs and players not in spectator.
func pushableEntity(e world.Entity) bool {
	switch v := e.(type) {
	case *player.Player:
		return v.GameMode().HasCollision() && !v.Dead()
	case *mob.Mob:
		return !v.Dead()
	case *entity.Ent:
		_, ok := mob.Of(v)
		return ok
	}
	return false
}

// pushApart is entity.push(self) (Entity.push(Entity)): each is pushed 0.05 at most away from the other. A
// player is not pushed (its client pushes itself); a mob of the Bedrock engine is.
func pushApart(e world.Entity, self *Mob) {
	ep, sp := e.Position(), self.Position()
	xa, za := sp[0]-ep[0], sp[2]-ep[2]
	dd := math.Max(math.Abs(xa), math.Abs(za))
	if dd < float64(float32(0.01)) {
		return
	}
	dd = math.Sqrt(dd)
	xa /= dd
	za /= dd
	pow := math.Min(1/dd, 1)
	xa *= pow * float64(float32(0.05))
	za *= pow * float64(float32(0.05))
	if o, ok := Of(e); ok {
		if len(o.Passengers()) == 0 && o.pushable() {
			o.Push(mgl64.Vec3{-xa, 0, -za})
		}
	} else if bm, ok := mob.Of(e); ok && len(bm.Passengers()) == 0 {
		bm.SetVelocity(bm.Velocity().Add(mgl64.Vec3{-xa, 0, -za}))
		bm.SyncMovement()
	}
	if len(self.Passengers()) == 0 {
		self.Push(mgl64.Vec3{xa, 0, za})
	}
}

// Push is Entity.push(x, y, z): add to the velocity and show it at once.
func (m *Mob) Push(v mgl64.Vec3) {
	if math.IsNaN(v[0]) || math.IsNaN(v[1]) || math.IsNaN(v[2]) || math.IsInf(v.Len(), 0) {
		return
	}
	m.SetVelocity(m.Velocity().Add(v))
	m.SyncMovement()
}

// touchPlayers calls PlayerTouch for every player (not a spectator, alive) whose box grown by 1, 0.5, 1 touches the
// mob: Player.aiStep's touch(entity) for the entities around it.
func (m *Mob) touchPlayers(pt PlayerToucher) {
	bb := m.BBox()
	area := bb.GrowVec3(mgl64.Vec3{1, 0.5, 1})
	for e := range m.Tx().EntitiesWithin(area) {
		p, ok := e.(*player.Player)
		if !ok || p.Dead() || p.GameMode() == world.GameModeSpectator {
			continue
		}
		if !overlaps(boxOf(p).GrowVec3(mgl64.Vec3{1, 0.5, 1}), bb) {
			continue
		}
		pt.PlayerTouch(m, p)
		if m.gone() || m.Dead() {
			return
		}
	}
}
