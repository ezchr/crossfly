//go:build crossfly_full

package boss

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

// witherPattern is WitherBossSpawnUtilities::createWitherBuilder: the rows
// of the pattern, from the skulls down. '^' is a wither skeleton skull, '#'
// soul sand or soul soil, '~' air.
var witherPattern = [3]string{"^^^", "###", "~#~"}

// facingSteps are the unit steps of the Bedrock facings (Facing::getStepX/Y/Z)
// in their order: down, up, north, south, west, east.
var facingSteps = [6]cube.Pos{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

// facingOpposite is Facing::OPPOSITE_FACING.
var facingOpposite = [6]int{1, 0, 3, 2, 5, 4}

// patternMatch is a BuildMatch: the pattern's first entry (the corner
// skull) and the two facings it was matched with. Entry (row, col) is at
// origin + row*step(up) + col*(step(forward) x step(up)).
type patternMatch struct {
	origin      cube.Pos
	forward, up int
}

// at is the position of entry (row, col).
func (pm patternMatch) at(row, col int) cube.Pos {
	f, u := facingSteps[pm.forward], facingSteps[pm.up]
	cross := cube.Pos{f[1]*u[2] - f[2]*u[1], f[2]*u[0] - f[0]*u[2], f[0]*u[1] - f[1]*u[0]}
	return pm.origin.Add(cube.Pos{u[0]*row + cross[0]*col, u[1]*row + cross[1]*col, u[2]*row + cross[2]*col})
}

// fitsWitherEntry is BlockPatternBuilder::_fitsBlockPatternEntry for the
// wither pattern's characters.
func fitsWitherEntry(tx *world.Tx, c byte, p cube.Pos) bool {
	switch c {
	case ' ':
		return true
	case '^':
		return isWitherSkull(tx, p)
	case '#':
		switch tx.Block(p).(type) {
		case block.SoulSand, block.SoulSoil:
			return true
		}
		return false
	case '~':
		return isAir(tx, p)
	}
	return false
}

func isWitherSkull(tx *world.Tx, p cube.Pos) bool {
	s, ok := tx.Block(p).(block.Skull)
	return ok && s.Type == block.WitherSkeletonSkull()
}

// matchWitherPattern is BlockPatternBuilder::match(BlockPos): the first
// entry of the pattern is looked for in the box from pos-3 to pos (x, then
// y, then z, ascending), and the pattern is tried there in every
// orientation (forward 0..5, then up 0..5, never along each other), the
// whole pattern lying any way round.
func matchWitherPattern(tx *world.Tx, pos cube.Pos) (patternMatch, bool) {
	const reach = len(witherPattern)
	for x := pos[0] - reach; x <= pos[0]; x++ {
		for y := pos[1] - reach; y <= pos[1]; y++ {
			for z := pos[2] - reach; z <= pos[2]; z++ {
				o := cube.Pos{x, y, z}
				if !fitsAnyWitherEntry(tx, o) {
					continue
				}
				for f := range facingSteps {
					for u := range facingSteps {
						if u == f || u == facingOpposite[f] {
							continue
						}
						if pm := (patternMatch{origin: o, forward: f, up: u}); witherPatternAt(tx, pm) {
							return pm, true
						}
					}
				}
			}
		}
	}
	return patternMatch{}, false
}

func fitsAnyWitherEntry(tx *world.Tx, p cube.Pos) bool {
	for _, c := range []byte{'^', '#', '~'} {
		if fitsWitherEntry(tx, c, p) {
			return true
		}
	}
	return false
}

func witherPatternAt(tx *world.Tx, pm patternMatch) bool {
	for row, s := range witherPattern {
		for col := range len(s) {
			if !fitsWitherEntry(tx, s[col], pm.at(row, col)) {
				return false
			}
		}
	}
	return true
}

// TrySpawnWither is SkullBlock::checkMobSpawn, run when a wither skeleton
// skull is placed at pos (block.SetSkullSpawner hooks it into skull
// placement): not on peaceful nor within two blocks of the bottom of the
// world, the wither pattern (matchWitherPattern) is looked for around the
// skull. A wither spawns at the match's centre as BuildMatch works it out
// (the first entry plus 1 on x and on z whatever way the pattern lies, 2
// down, so a wither of a standing T appears a block beside its stem), turned
// 90 degrees when the pattern faces north or south; then the nine blocks of
// the pattern turn to air with their destroy particles and 120 snowball
// puffs rise around the wither. It reports whether a wither spawned.
func TrySpawnWither(tx *world.Tx, pos cube.Pos) bool {
	if sense.DifficultyID(tx) == 0 || pos[1] < tx.Range()[0]+2 || !isWitherSkull(tx, pos) {
		return false
	}
	pm, ok := matchWitherPattern(tx, pos)
	if !ok {
		return false
	}
	at := pm.origin.Vec3().Add(mgl64.Vec3{1.5, -2 + 0.05, 1.5})
	yaw := 0.0
	if pm.forward == 2 || pm.forward == 3 {
		yaw = 90
	}
	m, err := mob.Spawn(tx, WitherID, at, mob.SpawnOptions{Rotation: cube.Rotation{yaw, 0}})
	if err != nil {
		return false
	}
	for row, s := range witherPattern {
		for col := range len(s) {
			p := pm.at(row, col)
			if b := tx.Block(p); !isAir(tx, p) {
				tx.AddParticle(p.Vec3Centre(), particle.BlockBreak{Block: b})
			}
			tx.SetBlock(p, nil, nil)
		}
	}
	r := m.Rand()
	for range 120 {
		tx.AddParticle(at.Add(mgl64.Vec3{r.Float64(), r.Float64()*3.9 - 2, r.Float64()}), particle.SnowballPoof{})
	}
	return true
}
