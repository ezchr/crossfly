//go:build crossfly_full

// Package boss holds the components and hard-coded parts of the boss and
// special hostile mobs: the boss bar (minecraft:boss) with the ender
// dragon's and the wither's actor logic, the warden's anger, vibrations,
// suspicion and heartbeat, instant_despawn (the creaking crumbling), and
// the projectiles the bosses fire (wither skulls, dragon fireballs, breeze wind charges). The
// goals are in server/mob/goal/boss. See README.md.
package boss

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat"
	"github.com/df-mc/dragonfly/server/world"
)

func init() {
	mob.RegisterComponent("minecraft:boss", newBoss)
	mob.RegisterComponent(OnStartLanding, newEventTrigger)
	mob.RegisterComponent(OnStartTakeoff, newEventTrigger)

	mob.RegisterComponent("minecraft:anger_level", newAngerLevel)
	mob.RegisterComponent("minecraft:suspect_tracking", newSuspectTracking)
	mob.RegisterComponent("minecraft:vibration_listener", newVibrationListener)
	mob.RegisterComponent("minecraft:vibration_damper", newFlag)
	mob.RegisterComponent("minecraft:heartbeat", newHeartbeat)
	mob.RegisterComponent("minecraft:movement_sound_distance_offset", newSoundOffset)

	mob.RegisterComponent("minecraft:instant_despawn", newInstantDespawn)
	mob.RegisterComponent("minecraft:body_rotation_blocked", newBodyRotationBlocked)

	// EnderDragon::postAiStep: the dragon moves itself after travel.
	mob.RegisterSystem(mob.System{Name: "boss/dragon/still", Stage: mob.StageControls, Order: 900, Tick: dragonStill})
	mob.RegisterSystem(mob.System{Name: "boss/dragon", Stage: mob.StageTravel, Order: 0, Tick: dragonTick})
	// WitherBossPreAIStepSystem + newServerAiStep, then the freeze.
	mob.RegisterSystem(mob.System{Name: "boss/wither", Stage: mob.StageSensors, Order: 50, Tick: witherPre})
	mob.RegisterSystem(mob.System{Name: "boss/wither/freeze", Stage: mob.StageControls, Order: 900, Tick: witherFreeze})
	mob.RegisterSystem(mob.System{Name: "boss/warden", Stage: mob.StageEnvironment, Order: 50, Tick: wardenTick})
	mob.RegisterHurtHook(mob.HurtHook{Name: "boss/vibration", Hurt: vibrationHurtHook})

	// SkullItem::_useOn -> SkullBlock::checkMobSpawn.
	block.SetSkullSpawner(TrySpawnWither)

	combat.RegisterProjectile("minecraft:wither_skull", func(m *mob.Mob, t world.Entity, _ int, _ float64) bool {
		ShootWitherSkull(m, 0, t.Position(), false)
		return true
	})
	combat.RegisterProjectile("minecraft:wither_skull_dangerous", func(m *mob.Mob, t world.Entity, _ int, _ float64) bool {
		ShootWitherSkull(m, 0, t.Position(), true)
		return true
	})
	combat.RegisterProjectile("minecraft:dragon_fireball", func(m *mob.Mob, t world.Entity, _ int, _ float64) bool {
		return ShootDragonFireball(m, t)
	})
	combat.RegisterProjectile("minecraft:breeze_wind_charge_projectile", func(m *mob.Mob, t world.Entity, _ int, _ float64) bool {
		return ShootWindCharge(m, t)
	})
}
