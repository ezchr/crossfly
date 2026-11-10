//go:build crossfly_full

package core

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
)

// classWalkTargetValue is the _getWalkTargetValue override of a mob class
// (BDS PathfinderMob subclasses), when it has one.
func classWalkTargetValue(m *mob.Mob, pos cube.Pos) (float64, bool) {
	switch m.Identifier() {
	case "minecraft:strider":
		// Strider::_getWalkTargetValue: 10 where the block is lava, 0
		// anywhere else, so it strolls over lava and not onto land.
		if _, ok := m.Tx().Block(pos).(block.Lava); ok {
			return 10, true
		}
		return 0, true
	}
	return 0, false
}
