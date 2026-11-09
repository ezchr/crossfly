//go:build crossfly_full

// Package hostile holds the enderman (teleport, looked_at; take_block and
// place_block are goal/village's), ravager (break_blocks, ravager_blocked), hoglin/piglin
// (group_size, custom_hit_test) and breeze (reflect_projectiles) parts of
// server/mob/goal/misc (see its README.md).
package hostile

import "github.com/df-mc/dragonfly/server/mob"

func init() {
	mob.RegisterComponent("minecraft:teleport", newTeleport)
	mob.RegisterComponent("minecraft:looked_at", newLookedAt)
	mob.RegisterComponent("minecraft:break_blocks", newBreakBlocks)
	mob.RegisterComponent("minecraft:ravager_blocked", newRavagerBlocked)
	mob.RegisterComponent("minecraft:custom_hit_test", newCustomHitTest)
	mob.RegisterComponent("minecraft:group_size", newGroupSize)
	mob.RegisterComponent("minecraft:reflect_projectiles", newReflectProjectiles)

	sys := func(name string, stage mob.Stage, order int, f func(m *mob.Mob)) {
		mob.RegisterSystem(mob.System{Name: "misc-hostile/" + name, Stage: stage, Order: 200 + order, Tick: f})
	}
	sys("teleport", mob.StageSensors, 0, func(m *mob.Mob) {
		if c, ok := mob.ComponentOf[*Teleport](m); ok {
			c.tick(m)
		}
	})
	sys("looked_at", mob.StageSensors, 1, func(m *mob.Mob) {
		if c, ok := mob.ComponentOf[*LookedAt](m); ok {
			c.tick(m)
		}
	})
	sys("group_size", mob.StageSensors, 2, func(m *mob.Mob) {
		if c, ok := mob.ComponentOf[*GroupSize](m); ok {
			c.tick(m)
		}
	})
	sys("reflect_projectiles", mob.StageSensors, 3, func(m *mob.Mob) {
		if c, ok := mob.ComponentOf[*ReflectProjectiles](m); ok {
			c.tick(m)
		}
	})
	// The shield block is only visible right after the hit, which happens in
	// the ravager's goal step.
	sys("ravager_blocked", mob.StageGoals, 4, func(m *mob.Mob) {
		if c, ok := mob.ComponentOf[*RavagerBlocked](m); ok {
			c.tick(m)
		}
	})
	// Collisions are known after travel.
	sys("break_blocks", mob.StageTravel, 5, func(m *mob.Mob) {
		if c, ok := mob.ComponentOf[*BreakBlocks](m); ok && !m.Dead() {
			c.tick(m)
		}
	})
}
