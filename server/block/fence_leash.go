//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// fenceLeashBinder ties the mobs a user leads to a knot in the fence at pos, reporting whether any was tied
// (BDS FenceBlock::use -> LeadItem::bindPlayerMobs). Package mob/component/core installs it.
var fenceLeashBinder func(pos cube.Pos, tx *world.Tx, u item.User) bool

// SetFenceLeashBinder installs the function using a fence runs. Call it from init only.
func SetFenceLeashBinder(f func(pos cube.Pos, tx *world.Tx, u item.User) bool) { fenceLeashBinder = f }

// Activate ties the mobs the user leads to a knot in the fence. Without any, the item held is used as usual.
func (w WoodFence) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, _ *item.UseContext) bool {
	return fenceLeashBinder != nil && fenceLeashBinder(pos, tx, u)
}

// Activate ties the mobs the user leads to a knot in the fence. Without any, the item held is used as usual.
func (n NetherBrickFence) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, _ *item.UseContext) bool {
	return fenceLeashBinder != nil && fenceLeashBinder(pos, tx, u)
}
