//go:build crossfly_full

package jnether

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

// Summoning the wither (WitherSkullBlock.checkSpawn): a wither skeleton
// skull placed (not on peaceful) completes the pattern of three skulls on
// a T of soul sand or soul soil with air in its lower corners, in any
// orientation (BlockPattern.find). The blocks go (with their break
// particles) and a wither appears in the stem's block, 0.55 up, facing
// along the pattern, invulnerable while it charges up.

// witherRows is WitherSkullBlock.getOrCreateWitherFull, from the top.
var witherRows = [3]string{"^^^", "###", "~#~"}

// javaDirections are Direction.values(): down, up, north, south, west,
// east.
var javaDirections = [6]cube.Pos{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

// witherMatch is a BlockPatternMatch: the front top left corner and the
// directions it was found with.
type witherMatch struct {
	corner      cube.Pos
	forward, up int
}

// at is BlockPattern.translateAndRotate(corner, forwards, up, right, down,
// 0).
func (m witherMatch) at(right, down int) cube.Pos {
	f, u := javaDirections[m.forward], javaDirections[m.up]
	r := cube.Pos{f[1]*u[2] - f[2]*u[1], f[2]*u[0] - f[0]*u[2], f[0]*u[1] - f[1]*u[0]}
	return m.corner.Add(cube.Pos{-u[0]*down + r[0]*right, -u[1]*down + r[1]*right, -u[2]*down + r[2]*right})
}

// fitsWither is the pattern's predicate of c at p.
func fitsWither(tx *world.Tx, c byte, p cube.Pos) bool {
	switch c {
	case '^':
		s, ok := tx.Block(p).(block.Skull)
		return ok && s.Type == block.WitherSkeletonSkull()
	case '#':
		switch tx.Block(p).(type) {
		case block.SoulSand, block.SoulSoil:
			return true
		}
		return false
	case '~':
		_, ok := tx.Block(p).(block.Air)
		return ok
	}
	return true
}

// findWitherPattern is BlockPattern.find(level, pos): corners from pos to
// pos + 2 on every axis, every forwards/up pair that are not on one axis.
func findWitherPattern(tx *world.Tx, pos cube.Pos) (witherMatch, bool) {
	for x := pos[0]; x <= pos[0]+2; x++ {
		for y := pos[1]; y <= pos[1]+2; y++ {
			for z := pos[2]; z <= pos[2]+2; z++ {
				for f := range javaDirections {
					for u := range javaDirections {
						if u == f || u == f^1 {
							continue
						}
						m := witherMatch{corner: cube.Pos{x, y, z}, forward: f, up: u}
						if m.matches(tx) {
							return m, true
						}
					}
				}
			}
		}
	}
	return witherMatch{}, false
}

func (m witherMatch) matches(tx *world.Tx) bool {
	for down, row := range witherRows {
		for right := range len(row) {
			p := m.at(right, down)
			if p.OutOfBounds(tx.Range()) || !fitsWither(tx, row[right], p) {
				return false
			}
		}
	}
	return true
}

// SummonHooks is what wither summoning needs from Java mob mode.
type SummonHooks struct {
	// SpawnWither adds a wither (EntitySpawnReason.TRIGGERED) at pos facing
	// yaw, made invulnerable (WitherBoss.makeInvulnerable).
	SpawnWither func(tx *world.Tx, pos mgl64.Vec3, yaw float64) bool
}

// TrySummonWither is WitherSkullBlock.checkSpawn for a wither skeleton
// skull placed at pos (block.SetSkullSpawner). It reports whether a wither
// was summoned.
func TrySummonWither(tx *world.Tx, pos cube.Pos, h SummonHooks) bool {
	if difficultyID(tx) == 0 || pos[1] < tx.Range().Min() || h.SpawnWither == nil {
		return false
	}
	if !fitsWither(tx, '^', pos) {
		return false
	}
	m, ok := findWitherPattern(tx, pos)
	if !ok {
		return false
	}
	stem := m.at(1, 2)
	yaw := 90.0
	if f := javaDirections[m.forward]; f[0] != 0 {
		yaw = 0
	}
	// CarvedPumpkinBlock.clearPatternBlocks: air, with the break particles
	// (level event 2001).
	for down, row := range witherRows {
		for right := range len(row) {
			p := m.at(right, down)
			if b := tx.Block(p); blockID(b) != "air" {
				tx.AddParticle(p.Vec3Centre(), particle.BlockBreak{Block: b})
				tx.SetBlock(p, nil, nil)
			}
		}
	}
	return h.SpawnWither(tx, mgl64.Vec3{float64(stem[0]) + 0.5, float64(stem[1]) + 0.55, float64(stem[2]) + 0.5}, yaw)
}
