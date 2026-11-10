//go:build crossfly_full

package misc

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
)

func init() {
	mob.RegisterSystem(mob.System{Name: "misc/happy_ghast_heal", Stage: mob.StageSensors, Order: 300, Tick: happyGhastHeal})
}

// happyGhastHeal is HappyGhast::continuousHeal: a hurt happy ghast heals 1 HP every 600 ticks of its age,
// every 20 in the rain or snow or in the clouds.
//
// Java fallback: no Bedrock data. The happy ghast JSON has no healing component and no 26.60 code for it was
// found, so this follows Java Edition's HappyGhast.continuousHeal.
func happyGhastHeal(m *mob.Mob) {
	if m.Identifier() != "minecraft:happy_ghast" || m.Ticks()%20 != 0 || m.Health() >= m.MaxHealth() {
		return
	}
	if m.Ticks()%600 == 0 || happyGhastFastHealing(m) {
		m.Heal(1, HappyGhastHealingSource{})
	}
}

// happyGhastFastHealing reports whether it rains or snows on the happy ghast (Level.precipitationAt) or it is
// in the clouds (Entity.isInClouds: the overworld cloud layer, y 192 to 196).
func happyGhastFastHealing(m *mob.Mob) bool {
	tx := m.Tx()
	pos := cube.PosFromVec3(m.Position())
	if tx.RainingAt(pos) || tx.SnowingAt(pos) {
		return true
	}
	if tx.World().Dimension() != world.Overworld {
		return false
	}
	const cloudHeight = 192
	y := m.Position()[1]
	return y+m.BBox().Height() >= cloudHeight && y <= cloudHeight+4
}

// HappyGhastHealingSource is the healing source of a happy ghast healing over time.
type HappyGhastHealingSource struct{}

// HealingSource ...
func (HappyGhastHealingSource) HealingSource() {}
