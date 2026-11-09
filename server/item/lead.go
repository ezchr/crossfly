//go:build crossfly_full

package item

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Lead is a lead (leash): used on a mob that may be leashed it ties the mob to the user, used on a fence it
// ties every mob the user leads to a knot in the fence. Leashing is done by the mob (its minecraft:leashable
// component); the fence part by the function SetLeadBinder installs (package mob/component/core).
type Lead struct{}

// leadBinder ties the mobs user leads to the fence at pos, reporting whether any was tied (BDS
// LeadItem::bindPlayerMobs).
var leadBinder func(tx *world.Tx, user User, pos cube.Pos) bool

// SetLeadBinder installs the function a lead used on a fence runs. Call it from init only.
func SetLeadBinder(f func(tx *world.Tx, user User, pos cube.Pos) bool) { leadBinder = f }

// UseOnBlock ties the mobs the user leads to a knot when the block is a fence (BDS LeadItem::_useOn: the use
// succeeds on any fence, also without mobs, and the lead is not used up).
func (Lead) UseOnBlock(pos cube.Pos, _ cube.Face, _ mgl64.Vec3, tx *world.Tx, user User, _ *UseContext) bool {
	if !fence(tx.Block(pos)) {
		return false
	}
	if leadBinder != nil {
		leadBinder(tx, user, pos)
	}
	return true
}

// EncodeItem ...
func (Lead) EncodeItem() (name string, meta int16) {
	return "minecraft:lead", 0
}

func init() {
	mobItems = append(mobItems, func() { world.RegisterItem(Lead{}) })
}
