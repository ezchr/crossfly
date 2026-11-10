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

// throwItem throws s from m's eyes towards to, as the allay's
// GoAndGiveItemsToOwnerGoal::_attemptToGiveItem and
// GoAndGiveItemsToNoteblockGoal::tick do: Spawner::spawnItem at the eye
// position with a pickup delay of 5 ticks, then the item's motion is the
// unit vector from the mob's position to the target times the throw speed
// 0.2, its y times 1.5 (both set in the goal constructors).
func throwItem(m *mob.Mob, s item.Stack, to mgl64.Vec3) {
	d := to.Sub(m.Position())
	if d.Len() >= 1e-4 {
		d = d.Normalize().Mul(0.2)
	} else {
		d = mgl64.Vec3{}
	}
	d[1] *= 1.5
	opts := world.EntitySpawnOpts{Position: m.EyePosition(), Velocity: d}
	m.Tx().AddEntity(entity.NewItemPickupDelay(opts, s, 5*time.Second/20))
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

// playerEyeHeight is how far over its feet BDS puts a player (Actor::getPosition of a player is its
// eye position).
const playerEyeHeight = 1.62
