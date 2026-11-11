//go:build crossfly_full

package hostile

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/go-gl/mathgl/mgl64"
)

// panicGoal is PanicGoal (MOVE): after damage of a panic cause (shouldPanic,
// given by the mob) run to a random spot within 5 blocks (4 up or down), or,
// on fire, to water within 5 blocks.
type panicGoal struct {
	javamob.GoalBase
	speed  float64
	should func(m *javamob.Mob) bool
	pos    mgl64.Vec3
}

func newPanic(speed float64, should func(m *javamob.Mob) bool) *panicGoal {
	return &panicGoal{GoalBase: javamob.GoalBase{Control: javamob.FlagMove}, speed: speed, should: should}
}

func (g *panicGoal) CanUse(m *javamob.Mob) bool {
	if !g.should(m) {
		return false
	}
	if m.IsOnFire() {
		if p, ok := lookForWater(m, 5); ok {
			g.pos = mgl64.Vec3{float64(p[0]), float64(p[1]), float64(p[2])}
			return true
		}
	}
	p, ok := m.DefaultRandomPos(5, 4)
	if ok {
		g.pos = p
	}
	return ok
}

func (g *panicGoal) Start(m *javamob.Mob) { m.Navigation().MoveTo(m, g.pos, 1, g.speed) }

func (g *panicGoal) CanContinueToUse(m *javamob.Mob) bool { return !m.Navigation().Done() }

// lookForWater is PanicGoal.lookForWater: nothing if the mob stands in a
// block with a collision shape, else the first water block of
// findBlocksInBoxByManhattanDistance(pos, xz, 1).
func lookForWater(m *javamob.Mob, xz int) (cube.Pos, bool) {
	tx := m.Tx()
	at := blockPos(m.Position())
	if len(tx.Block(at).Model().BBox(at, tx)) > 0 {
		return cube.Pos{}, false
	}
	for _, p := range manhattanBox(at, xz, 1) {
		if l, ok := tx.Liquid(p); ok && isWaterLiquid(l) {
			return p, true
		}
	}
	return cube.Pos{}, false
}

// manhattanBox is BlockPos.withinManhattan(pos, xz, y, xz) in its order: by
// growing Manhattan distance, then y, x, z as Java walks them.
func manhattanBox(c cube.Pos, xz, y int) []cube.Pos {
	var out []cube.Pos
	maxDepth := xz + y + xz
	for depth := 0; depth <= maxDepth; depth++ {
		for dy := 0; dy <= min(y, depth); dy++ {
			for _, sy := range signs(dy) {
				rest := depth - dy
				for dx := 0; dx <= min(xz, rest); dx++ {
					dz := rest - dx
					if dz > xz {
						continue
					}
					for _, sx := range signs(dx) {
						for _, sz := range signs(dz) {
							out = append(out, cube.Pos{c[0] + sx*dx, c[1] + sy*dy, c[2] + sz*dz})
						}
					}
				}
			}
		}
	}
	return out
}

func signs(v int) []int {
	if v == 0 {
		return []int{1}
	}
	return []int{1, -1}
}
