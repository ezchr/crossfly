//go:build crossfly_full

package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// BlockBBoxesAround returns the world-space collision boxes of the blocks
// around box (box grown by 0.25), the boxes MovementComputer.CheckCollision
// collides with. It is exported for the mob physics in server/mob.
func BlockBBoxesAround(tx *world.Tx, box cube.BBox) []cube.BBox {
	return blockBBoxsAround(tx, box)
}
