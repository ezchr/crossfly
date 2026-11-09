package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// skullSpawner is the function SetSkullSpawner set.
var skullSpawner func(tx *world.Tx, pos cube.Pos) bool

// SetSkullSpawner sets the function run when a wither skeleton skull is placed (BDS SkullItem::_useOn calls
// SkullBlock::checkMobSpawn): Crossfly Full's mobs build the wither from it. Without mobs it is unset and
// placing a skull spawns nothing. Call it once, from an init function.
func SetSkullSpawner(f func(tx *world.Tx, pos cube.Pos) bool) { skullSpawner = f }

// skullPlaced runs the skull spawner for the skull just placed at pos.
func skullPlaced(tx *world.Tx, pos cube.Pos) {
	if skullSpawner == nil {
		return
	}
	if s, ok := tx.Block(pos).(Skull); ok && s.Type == WitherSkeletonSkull() {
		skullSpawner(tx, pos)
	}
}
