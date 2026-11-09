//go:build crossfly_full

package mob

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

// landHook lands a mob that fell dist blocks: the blocks it lands on (hay
// bale, slime, bed) and the fall damage of its class.
var landHook func(m *Mob, dist float64)

// SetLand installs how a mob takes a landing after a fall. Only package
// mob/physics calls it: its travel lands the mobs it moves itself, the hook
// lands the mobs a rider's client moves (ClientVehicleMove), which skip
// travel.
func SetLand(f func(m *Mob, dist float64)) {
	if landHook != nil {
		panic("mob: land already set")
	}
	landHook = f
}

// clientFall keeps the fall distance of a mob its controlling rider's client
// just moved dy blocks up, as travel does for the mobs the server moves
// (Actor::checkFallDamage): a liquid clears it, a move down adds to it, and
// standing on a block again lands the mob. OnGround follows the client's
// position.
func (m *Mob) clientFall(dy float64) {
	p := &m.s.physics
	tx := m.Tx()
	box := m.BBox()
	if boxInLiquid(tx, box) {
		p.FallDistance = 0
		p.OnGround = false
		return
	}
	if dy < 0 {
		p.FallDistance -= dy
	}
	p.OnGround = standing(tx, box)
	if !p.OnGround || p.FallDistance <= 0 {
		return
	}
	dist := p.FallDistance
	p.FallDistance = 0
	if landHook != nil {
		landHook(m, dist)
	}
}

// standing reports whether a block's collision box is right under box
// (within 0.1).
func standing(tx *world.Tx, box cube.BBox) bool {
	floor := cube.Box(box.Min()[0]+1e-4, box.Min()[1]-0.1, box.Min()[2]+1e-4, box.Max()[0]-1e-4, box.Min()[1], box.Max()[2]-1e-4)
	for _, bb := range entity.BlockBBoxesAround(tx, floor) {
		if bb.IntersectsWith(floor) {
			return true
		}
	}
	return false
}

// boxInLiquid reports whether any block box is in holds water or lava.
func boxInLiquid(tx *world.Tx, box cube.BBox) bool {
	box = box.Grow(-1e-3)
	minX, minY, minZ := int(math.Floor(box.Min()[0])), int(math.Floor(box.Min()[1])), int(math.Floor(box.Min()[2]))
	maxX, maxY, maxZ := int(math.Floor(box.Max()[0])), int(math.Floor(box.Max()[1])), int(math.Floor(box.Max()[2]))
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			for z := minZ; z <= maxZ; z++ {
				if _, ok := tx.Liquid(cube.Pos{x, y, z}); ok {
					return true
				}
			}
		}
	}
	return false
}
