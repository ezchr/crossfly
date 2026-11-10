//go:build crossfly_full

package jmob

import (
	"slices"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/jbrain"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Sensor is a sensor of a Java mob's brain.
type Sensor = jbrain.Sensor[*javamob.Mob]

// sensorFunc is a stateless sensor.
type sensorFunc struct {
	req   []jbrain.MemoryID
	sense func(c *jbrain.Ctx, m *javamob.Mob)
}

func (s sensorFunc) Requires() []jbrain.MemoryID         { return s.req }
func (s sensorFunc) Sense(c *jbrain.Ctx, m *javamob.Mob) { s.sense(c, m) }
func ids(ms ...interface{ ID() jbrain.MemoryID }) []jbrain.MemoryID {
	out := make([]jbrain.MemoryID, len(ms))
	for i, m := range ms {
		out[i] = m.ID()
	}
	return out
}

// newType registers nothing: SensorTypes are plain values shared by the
// providers that list them.
func newType(name, key string, rate int, f func() Sensor) *jbrain.SensorType[*javamob.Mob] {
	return &jbrain.SensorType[*javamob.Mob]{Name: "minecraft:" + name, Key: key, ScanRate: rate, New: f}
}

// IsLiving reports whether e is a LivingEntity (a player or a mob).
func IsLiving(e world.Entity) bool {
	_, ok := e.(entity.Living)
	return ok
}

// IsAlive is Entity.isAlive for living entities.
func IsAlive(e world.Entity) bool {
	if l, ok := e.(entity.Living); ok {
		return !l.Dead()
	}
	return true
}

func init() {
	jbrain.IsLiving = IsLiving
	jbrain.IsAlive = IsAlive
}

// IsEntityTargetable is Sensor.isEntityTargetable: a non-combat check within
// the follow range, ignoring invisibility for the current attack target.
func IsEntityTargetable(c *jbrain.Ctx, m *javamob.Mob, e world.Entity) bool {
	tc := javamob.ForNonCombat(m.FollowRange())
	if t, ok := jbrain.Get(c.Mem, jbrain.MemAttackTarget); ok && t == e.H() {
		tc.IgnoreInvisibility = true
	}
	return tc.Test(m, e)
}

// IsEntityAttackable is Sensor.isEntityAttackable.
func IsEntityAttackable(c *jbrain.Ctx, m *javamob.Mob, e world.Entity) bool {
	tc := javamob.ForCombat(m.FollowRange())
	if t, ok := jbrain.Get(c.Mem, jbrain.MemAttackTarget); ok && t == e.H() {
		tc.IgnoreInvisibility = true
	}
	return tc.Test(m, e)
}

func distSq(a, b mgl64.Vec3) float64 { return a.Sub(b).LenSqr() }

// NearestLivingEntities is SensorType.NEAREST_LIVING_ENTITIES
// (NearestLivingEntitySensor): living entities in the bounding box grown by
// the follow range, closest first, and the visible ones among them.
var NearestLivingEntities = newType("nearest_living_entities", "nearestlivingentitysensor", 0, func() Sensor {
	return sensorFunc{req: ids(jbrain.MemNearestLivingEntities, jbrain.MemNearestVisibleLivingEntities), sense: senseNearestLiving}
})

func senseNearestLiving(c *jbrain.Ctx, m *javamob.Mob) {
	r := m.FollowRange()
	box := m.BBox().Grow(r)
	pos := m.Position()
	type cand struct {
		h *world.EntityHandle
		d float64
	}
	var list []cand
	for e := range c.Tx.EntitiesWithin(box) {
		if e.H() == m.H() || !IsLiving(e) || !IsAlive(e) {
			continue
		}
		list = append(list, cand{e.H(), distSq(e.Position(), pos)})
	}
	slices.SortStableFunc(list, func(a, b cand) int {
		switch {
		case a.d < b.d:
			return -1
		case a.d > b.d:
			return 1
		}
		return 0
	})
	hs := make([]*world.EntityHandle, len(list))
	for i, x := range list {
		hs[i] = x.h
	}
	jbrain.Set(c.Mem, jbrain.MemNearestLivingEntities, hs)
	body := m.H()
	jbrain.Set(c.Mem, jbrain.MemNearestVisibleLivingEntities, jbrain.NewVisibleEntities(hs, func(tx *world.Tx, h *world.EntityHandle) bool {
		e, ok := h.Entity(tx)
		if !ok {
			return false
		}
		bm, ok := javamob.OfHandle(tx, body)
		if !ok {
			return false
		}
		return IsEntityTargetable(RunCtx(bm), bm, e)
	}))
}

// NearestPlayers is SensorType.NEAREST_PLAYERS (PlayerSensor).
var NearestPlayers = newType("nearest_players", "playersensor", 0, func() Sensor {
	return sensorFunc{req: ids(jbrain.MemNearestPlayers, jbrain.MemNearestVisiblePlayer, jbrain.MemNearestVisibleAttackablePlayer, jbrain.MemNearestVisibleAttackablePlayers), sense: sensePlayers}
})

func sensePlayers(c *jbrain.Ctx, m *javamob.Mob) {
	pos := m.Position()
	dist := m.FollowRange()
	var players []*player.Player
	for e := range c.Tx.Players() {
		p, ok := e.(*player.Player)
		if !ok || !p.GameMode().Visible() {
			continue
		}
		if distSq(p.Position(), pos) < dist*dist {
			players = append(players, p)
		}
	}
	slices.SortStableFunc(players, func(a, b *player.Player) int {
		da, db := distSq(a.Position(), pos), distSq(b.Position(), pos)
		switch {
		case da < db:
			return -1
		case da > db:
			return 1
		}
		return 0
	})
	all := make([]*world.EntityHandle, 0, len(players))
	var visible []*player.Player
	for _, p := range players {
		all = append(all, p.H())
		if IsEntityTargetable(c, m, p) {
			visible = append(visible, p)
		}
	}
	jbrain.Set(c.Mem, jbrain.MemNearestPlayers, all)
	if len(visible) > 0 {
		jbrain.Set(c.Mem, jbrain.MemNearestVisiblePlayer, visible[0].H())
	} else {
		c.Mem.Erase(jbrain.MemNearestVisiblePlayer.ID())
	}
	var attackable []*world.EntityHandle
	for _, p := range visible {
		if IsEntityAttackable(c, m, p) {
			attackable = append(attackable, p.H())
		}
	}
	jbrain.Set(c.Mem, jbrain.MemNearestVisibleAttackablePlayers, attackable)
	if len(attackable) > 0 {
		jbrain.Set(c.Mem, jbrain.MemNearestVisibleAttackablePlayer, attackable[0])
	} else {
		c.Mem.Erase(jbrain.MemNearestVisibleAttackablePlayer.ID())
	}
}

// Dummy is SensorType.DUMMY.
var Dummy = newType("dummy", "dummysensor", 0, func() Sensor {
	return sensorFunc{sense: func(*jbrain.Ctx, *javamob.Mob) {}}
})

// IsInWater is SensorType.IS_IN_WATER.
var IsInWater = newType("is_in_water", "isinwatersensor", 0, func() Sensor {
	return sensorFunc{req: ids(jbrain.MemIsInWater), sense: func(c *jbrain.Ctx, m *javamob.Mob) {
		if m.InWater() {
			jbrain.Set(c.Mem, jbrain.MemIsInWater, jbrain.Unit{})
		} else {
			c.Mem.Erase(jbrain.MemIsInWater.ID())
		}
	}}
})

// NearestVisible returns the closest visible living entity passing match
// (NearestVisibleLivingEntitySensor.getNearestEntity).
func NearestVisible(c *jbrain.Ctx, match func(e world.Entity) bool) (*world.EntityHandle, bool) {
	v, ok := jbrain.Get(c.Mem, jbrain.MemNearestVisibleLivingEntities)
	if !ok {
		return nil, false
	}
	return v.FindClosest(c.Tx, func(h *world.EntityHandle) bool {
		e, ok := h.Entity(c.Tx)
		return ok && match(e)
	})
}

// VisibleSensor builds a NearestVisibleLivingEntitySensor: it sets mem to
// the closest visible living entity that passes match.
func VisibleSensor(name, key string, mem jbrain.Memory[*world.EntityHandle], match func(c *jbrain.Ctx, m *javamob.Mob, e world.Entity) bool) *jbrain.SensorType[*javamob.Mob] {
	return newType(name, key, 0, func() Sensor {
		return sensorFunc{req: ids(mem, jbrain.MemNearestVisibleLivingEntities), sense: func(c *jbrain.Ctx, m *javamob.Mob) {
			h, ok := NearestVisible(c, func(e world.Entity) bool { return match(c, m, e) })
			jbrain.SetOpt(c.Mem, mem, h, ok)
		}}
	})
}

// HurtBy is SensorType.HURT_BY (HurtBySensor): the last damage source (kept
// 40 ticks by LivingEntity.getLastDamageSource) and the living entity that
// caused it, forgotten once it dies.
var HurtBy = newType("hurt_by", "hurtbysensor", 0, func() Sensor {
	return sensorFunc{req: ids(jbrain.MemHurtBy, jbrain.MemHurtByEntity), sense: senseHurtBy}
})

func senseHurtBy(c *jbrain.Ctx, m *javamob.Mob) {
	if src, ok := LastDamageSource(m); ok {
		jbrain.Set(c.Mem, jbrain.MemHurtBy, src)
		if att, ok := SourceEntity(src); ok && IsLiving(att) {
			jbrain.Set(c.Mem, jbrain.MemHurtByEntity, att.H())
		}
	} else {
		c.Mem.Erase(jbrain.MemHurtBy.ID())
	}
	if h, ok := jbrain.Get(c.Mem, jbrain.MemHurtByEntity); ok {
		if e, ok := h.Entity(c.Tx); !ok || !IsAlive(e) {
			c.Mem.Erase(jbrain.MemHurtByEntity.ID())
		}
	}
}

// SourceEntity is DamageSource.getEntity: the attacker, or the shooter of a
// projectile.
func SourceEntity(src world.DamageSource) (world.Entity, bool) {
	switch s := src.(type) {
	case entity.AttackDamageSource:
		return s.Attacker, s.Attacker != nil
	case entity.ProjectileDamageSource:
		return s.Owner, s.Owner != nil
	}
	return nil, false
}
