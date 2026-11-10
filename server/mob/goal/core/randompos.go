//go:build crossfly_full

package core

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// RandomPos picks a random destination around m, the best of attempts
// candidates by walk target value (BDS RandomPos::generateRandomPos):
//
//   - each candidate is the mob's block position plus dx, dz in [-xz, xz] and
//     dy in [-y, y] (+ yOffset);
//   - away, when set, flips candidates that point towards it (getPosAvoid
//     passes position - avoid);
//   - walkers snap the candidate to the ground within y blocks (the first
//     non-solid block above a solid one); fliers, swimmers and mobs in water
//     or lava take it as it is;
//   - the candidate with the highest walk target value wins (the first one
//     on ties).
//
// The home restriction (minecraft:home) is not applied. It returns false
// when no candidate was valid.
func RandomPos(m *mob.Mob, xz, y, yOffset int, away *mgl64.Vec3, attempts int) (mgl64.Vec3, bool) {
	if attempts <= 0 || xz < 1 {
		return mgl64.Vec3{}, false
	}
	pos := m.Position()
	base := cube.PosFromVec3(pos)
	fly := m.HasComponent("minecraft:can_fly")
	swim := m.HasComponent("minecraft:navigation.swim") || (m.HasComponent("minecraft:navigation.generic") && m.Physics().InWater)
	liquid := m.Physics().InWater || m.Physics().InLava
	best, found := math.Inf(-1), false
	var bestPos cube.Pos
	r := m.Rand()
	for i := 0; i < attempts; i++ {
		dx := r.IntN(2*xz+1) - xz
		dz := r.IntN(2*xz+1) - xz
		dy := r.IntN(2*y+1) + yOffset - y
		if away != nil && float64(dz)*away[2]+float64(dx)*away[0] < 0 {
			dx, dz = -dx, -dz
		}
		c := base.Add(cube.Pos{dx, dy, dz})
		if !(swim || fly || liquid) {
			var ok bool
			if c, ok = snapToGround(m.Tx(), c, base[1]-y, base[1]+y); !ok {
				continue
			}
		}
		if v := WalkTargetValue(m, c); v > best {
			best, bestPos, found = v, c, true
		}
	}
	if !found {
		return mgl64.Vec3{}, false
	}
	return mgl64.Vec3{float64(bestPos[0]), float64(bestPos[1]), float64(bestPos[2])}, true
}

// snapToGround moves pos to the first non-solid block standing on a solid
// one, searching down to lo, then up to hi (BDS RandomPos::snapToGround).
func snapToGround(tx *world.Tx, pos cube.Pos, lo, hi int) (cube.Pos, bool) {
	solid := func(p cube.Pos) bool { return isSolid(tx, p) }
	here, above := solid(pos), solid(pos.Side(cube.FaceUp))
	if here && !above {
		return pos.Side(cube.FaceUp), true
	}
	// Walk down while the block above was not solid: the first solid block
	// under a free one is the ground.
	p := pos
	prev := above
	for p[1] >= lo {
		s := solid(p)
		if s && !prev {
			return p.Side(cube.FaceUp), true
		}
		prev = s
		p[1]--
	}
	// Then walk up from the start: the first free block over a solid one.
	p = pos
	below := here
	for p[1] < hi {
		p[1]++
		s := solid(p)
		if !s && below {
			return p, true
		}
		below = s
	}
	return pos, false
}

// isSolid reports whether a mob could stand on the block (it has a
// collision box). Liquids are not solid.
func isSolid(tx *world.Tx, pos cube.Pos) bool {
	if pos[1] < tx.Range()[0] || pos[1] > tx.Range()[1] {
		return false
	}
	b := tx.Block(pos)
	if _, ok := b.(block.Air); ok {
		return false
	}
	if _, ok := b.(world.Liquid); ok {
		return false
	}
	return len(b.Model().BBox(pos, tx)) > 0
}

// WalkTargetValue is Mob::getWalkTargetValue, how much a mob likes standing
// at pos: animals (spawn category "creature") like grass (10) and light,
// monsters like the dark, magma is avoided (-0.5), others don't care (0).
func WalkTargetValue(m *mob.Mob, pos cube.Pos) float64 {
	if v, ok := classWalkTargetValue(m, pos); ok {
		return v // walk_target_class.go
	}
	tx := m.Tx()
	below := tx.Block(pos.Side(cube.FaceDown))
	animal := m.Definition().Description().SpawnCategory == "creature"
	monster := m.HasFamily("monster")
	if !animal && !monster {
		return 0
	}
	if animal {
		if _, ok := below.(block.Grass); ok {
			return 10
		}
	}
	if _, ok := below.(block.Magma); ok {
		return -0.5
	}
	b := brightness(tx, pos)
	if animal {
		return b - 0.5
	}
	return 0.5 - b
}

// brightness is BlockSource::getBrightness: the light level through the
// overworld brightness curve, 0..1.
func brightness(tx *world.Tx, pos cube.Pos) float64 {
	f := float64(tx.Light(pos)) / 15
	return f / (4 - 3*f)
}
