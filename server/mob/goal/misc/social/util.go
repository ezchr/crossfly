//go:build crossfly_full

package social

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// timeOfDay is the celestial angle of the world time (BDS
// Dimension::getTimeOfDay, Java getTimeOfDay): 0 noon, 0.25 sunset, 0.5
// midnight, 0.75 sunrise, smoothed as the game does.
func timeOfDay(tx *world.Tx) float64 {
	t := tx.World().Time() % 24000
	if t < 0 {
		t += 24000
	}
	f := float64(t)/24000 - 0.25
	if f < 0 {
		f++
	}
	if f > 1 {
		f--
	}
	f2 := 1 - (math.Cos(f*math.Pi)+1)/2
	return f + (f2-f)/3
}

// throwItem throws s from m's eyes towards to (BDS
// GoAndGiveItemsToOwnerGoal::_attemptToGiveItem; the speed follows Java
// BehaviorUtils.throwItem, 0.2 blocks a tick along the direction plus a
// little lift), with a pickup delay.
func throwItem(m *mob.Mob, s item.Stack, to mgl64.Vec3, pickupDelay time.Duration) {
	from := m.EyePosition().Sub(mgl64.Vec3{0, 0.3, 0})
	d := to.Sub(from)
	if d.Len() > 1e-4 {
		d = d.Normalize().Mul(0.2)
	}
	d[1] += 0.1
	opts := world.EntitySpawnOpts{Position: from, Velocity: d}
	if pickupDelay > 0 {
		m.Tx().AddEntity(entity.NewItemPickupDelay(opts, s, pickupDelay))
		return
	}
	m.Tx().AddEntity(entity.NewItem(opts, s))
}

// entityOf opens h in m's transaction.
func entityOf(m *mob.Mob, h *world.EntityHandle) (world.Entity, bool) {
	if h == nil {
		return nil, false
	}
	return h.Entity(m.Tx())
}

// blockCentre is the bottom centre of block p.
func blockCentre(p cube.Pos) mgl64.Vec3 {
	return mgl64.Vec3{float64(p[0]) + 0.5, float64(p[1]), float64(p[2]) + 0.5}
}

// yawDir is the horizontal unit vector a yaw (degrees) faces.
func yawDir(yaw float64) mgl64.Vec3 {
	r := mgl64.DegToRad(yaw)
	return mgl64.Vec3{-math.Sin(r), 0, math.Cos(r)}
}
