//go:build !crossfly_full

package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// cushionFullOutline returns the outline of blocks only Crossfly Full has: None here.
func cushionFullOutline(world.Block) ([]cube.BBox, bool) {
	return nil, false
}
