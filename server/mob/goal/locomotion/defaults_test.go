//go:build crossfly_full

package locomotion

import (
	"testing"

	"github.com/df-mc/dragonfly/server/mob"
)

func build[T mob.Goal](t *testing.T, key string, f mob.GoalFactory, raw string) T {
	t.Helper()
	spec, err := mob.ParseGoalSpec(key, []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	g, err := f(spec)
	if err != nil {
		t.Fatalf("%v: %v", key, err)
	}
	v, ok := g.(T)
	if !ok {
		t.Fatalf("%v: goal is %T", key, g)
	}
	return v
}

func eq[V comparable](t *testing.T, name string, got, want V) {
	t.Helper()
	if got != want {
		t.Errorf("%v = %v, want %v", name, got, want)
	}
}

// TestBDSDefaults checks every goal's defaults (JSON without fields) against
// the BDS 1.21.3 definitions (LegacyGoalDefinition::parse, the *Definition
// constructors) and its default control flags.
func TestBDSDefaults(t *testing.T) {
	rs := build[*randomSwim](t, "minecraft:behavior.random_swim", newRandomSwim, `{}`)
	eq(t, "random_swim speed", rs.speed, 1.0)
	eq(t, "random_swim xz", rs.xz, 10)
	eq(t, "random_swim y", rs.y, 7)
	eq(t, "random_swim interval", rs.interval, 120)
	eq(t, "random_swim avoid_surface", rs.avoidSurface, true)
	eq(t, "random_swim flags", rs.Flags(), mob.FlagMove|mob.FlagLook)

	si := build[*swimIdle](t, "minecraft:behavior.swim_idle", newSwimIdle, `{}`)
	eq(t, "swim_idle ticks", si.idle, 100)
	eq(t, "swim_idle success_rate", si.rate, 0.1)
	eq(t, "swim_idle flags", si.Flags(), mob.FlagMove)

	sw := build[*swimWander](t, "minecraft:behavior.swim_wander", newSwimWander, `{}`)
	eq(t, "swim_wander interval", sw.interval, 0.01)
	eq(t, "swim_wander look_ahead", sw.lookAhead, 5.0)
	eq(t, "swim_wander speed", sw.speed, 1.0)
	eq(t, "swim_wander ticks", sw.maxTicks, 100)
	eq(t, "swim_wander flags", sw.Flags(), mob.FlagMove)

	mw := build[*moveToBlock](t, "minecraft:behavior.move_to_water", newMoveTo(moveToLiquid, materialWater), `{}`)
	eq(t, "move_to_water speed", mw.speed, 1.0)
	eq(t, "move_to_water search_range", mw.searchRange, 0)
	eq(t, "move_to_water search_height", mw.searchHeight, 1)
	eq(t, "move_to_water search_count", mw.searchCount, 10)
	eq(t, "move_to_water goal_radius²", mw.goalRadiusSq, 0.25)
	eq(t, "move_to_water material", mw.material, materialWater)
	eq(t, "move_to_water flags", mw.Flags(), mob.FlagMove|mob.FlagLook)
	ml := build[*moveToBlock](t, "minecraft:behavior.move_to_liquid", newMoveTo(moveToLiquid, materialAny), `{"material_type":"Lava"}`)
	eq(t, "move_to_liquid material", ml.material, materialLava)

	rl := build[*riseToLiquidLevel](t, "minecraft:behavior.rise_to_liquid_level", newRiseToLiquidLevel, `{}`)
	eq(t, "rise offset", rl.offset, 0.0)
	eq(t, "rise flags", rl.Flags(), mob.FlagJump)

	br := build[*swimUpForBreath](t, "minecraft:behavior.swim_up_for_breath", newSwimUpForBreath, `{}`)
	eq(t, "breathe_air speed", br.speed, 1.4)
	eq(t, "breathe_air radius", br.radius, 4)

	we := build[*swimWithEntity](t, "minecraft:behavior.swim_with_entity", newSwimWithEntity, `{}`)
	eq(t, "swim_with_entity success_rate", we.rate, 0.1)
	eq(t, "swim_with_entity interval", we.interval, 10)
	eq(t, "swim_with_entity catch_up_threshold", we.catchUpThreshold, 12.0)
	eq(t, "swim_with_entity catch_up_multiplier", we.catchUpMul, 2.5)
	eq(t, "swim_with_entity speed", we.speedMul, 1.5)
	eq(t, "swim_with_entity search_range", we.searchRange, 20.0)
	eq(t, "swim_with_entity stop_distance", we.stopDistance, 5.0)

	rb := build[*randomBreach](t, "minecraft:behavior.random_breach", newRandomBreach, `{"cooldown_time":2}`)
	eq(t, "random_breach interval", rb.interval, 120)
	eq(t, "random_breach cooldown", rb.cooldownTicks, 40)

	ft := build[*findUnderwaterTreasure](t, "minecraft:behavior.find_underwater_treasure", newFindUnderwaterTreasure, `{}`)
	eq(t, "treasure stop_distance", ft.stopDist, 2)
	eq(t, "treasure flags", ft.Flags(), mob.FlagMove)

	pd := build[*playDead](t, "minecraft:behavior.play_dead", newPlayDead, `{}`)
	eq(t, "play_dead duration", pd.duration, 20)
	eq(t, "play_dead random_start_chance", pd.startChance, 1.0)
	eq(t, "play_dead all causes", pd.causes == nil, true)

	fw := build[*floatWander](t, "minecraft:behavior.float_wander", newFloatWander, `{}`)
	eq(t, "float_wander xz", fw.xz, 10.0)
	eq(t, "float_wander y", fw.y, 7.0)
	eq(t, "float_wander flags", fw.Flags(), mob.FlagMove) // float_wander_has_move_control (26.60)
	fwOff := build[*floatWander](t, "minecraft:behavior.float_wander", newFloatWander, `{"float_wander_has_move_control":false}`)
	eq(t, "float_wander flags without move control", fwOff.Flags(), mob.ControlFlags(0))
	eq(t, "float_wander use_home_position_restriction", fw.useHome, true)
	eq(t, "float_wander surface_xz_dist", fw.surfXZ, 8)
	eq(t, "float_wander surface_y_dist", fw.surfY, 8)
	eq(t, "float_wander navigate_around_surface", fw.aroundSurface, false)

	rh := build[*randomHover](t, "minecraft:behavior.random_hover", newRandomHover, `{"hover_height":[1,4]}`)
	eq(t, "random_hover interval", rh.interval, 120)
	eq(t, "random_hover hover max", rh.hover.Max, 4.0)
	eq(t, "random_hover flags", rh.Flags(), mob.FlagMove)

	rf := build[*randomFly](t, "minecraft:behavior.random_fly", newRandomFly, `{}`)
	eq(t, "random_fly trees", rf.trees, true)
	eq(t, "random_fly flags", rf.Flags(), mob.FlagMove|mob.FlagLook)

	ca := build[*circleAroundAnchor](t, "minecraft:behavior.circle_around_anchor", newCircleAroundAnchor, `{}`)
	eq(t, "circle radius_change", ca.radiusChange, 1.0)
	eq(t, "circle radius_adjustment_chance", ca.radiusChance, 0.004)
	eq(t, "circle height_adjustment_chance", ca.heightChance, 0.002857)
	eq(t, "circle goal_radius²", ca.goalRadiusSq, 0.25)
	eq(t, "circle angle_change", ca.angleChange, 15.0)
	eq(t, "circle radius max", ca.radiusRange.Max, 15.0)

	sa := build[*swoopAttack](t, "minecraft:behavior.swoop_attack", newSwoopAttack, `{}`)
	eq(t, "swoop damage_reach", sa.reach, 0.2)
	eq(t, "swoop delay min", sa.delayMin, 200.0)
	eq(t, "swoop delay max", sa.delayMax, 400.0)

	sf := build[*slimeFloat](t, "minecraft:behavior.slime_float", newSlimeFloat, `{}`)
	eq(t, "slime_float chance", sf.chance, 0.8)
	eq(t, "slime_float speed", sf.speed, 1.2)
	eq(t, "slime_float flags", sf.Flags(), mob.FlagMove|mob.FlagJump)
	sr := build[*slimeRandomDirection](t, "minecraft:behavior.slime_random_direction", newSlimeRandomDirection, `{}`)
	eq(t, "slime_random_direction min", sr.minTicks, 40)
	eq(t, "slime_random_direction add", sr.addTicks, 60)
	eq(t, "slime_random_direction turn", sr.turnRange, 360)
	eq(t, "slime_random_direction flags", sr.Flags(), mob.FlagLook)
	st := build[*slimeAttack](t, "minecraft:behavior.slime_attack", newSlimeAttack, `{}`)
	eq(t, "slime_attack tired", st.tired, 300)
	eq(t, "slime_attack flags", st.Flags(), mob.FlagLook)

	ga := build[*guardianAttack](t, "minecraft:behavior.guardian_attack", newGuardianAttack, `{}`)
	eq(t, "guardian magic", ga.magic, 1.0)
	eq(t, "guardian hard extra", ga.hardExtra, 2.0)
	eq(t, "guardian elder extra", ga.elderExtra, 2.0)
	eq(t, "guardian min_distance", ga.minDist, 3.0)
	eq(t, "guardian flags", ga.Flags(), mob.FlagMove|mob.FlagLook)

	ac := build[*aquaticChargeAttack](t, "minecraft:behavior.aquatic_charge_attack", newAquaticChargeAttack, `{"charge_cooldown_time":{"min":4,"max":4}}`)
	eq(t, "charge cooldown", ac.cooldownRange.Max, 4.0)
	eq(t, "charge speed", ac.speed, 1.2)
}
