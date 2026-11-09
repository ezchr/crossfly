//go:build crossfly_full

package boss

import (
	"encoding/json"
	"testing"

	"github.com/df-mc/dragonfly/server/mob"
)

func build(t *testing.T, f mob.GoalFactory, key, raw string) mob.Goal {
	t.Helper()
	spec, err := mob.ParseGoalSpec(key, json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	g, err := f(spec)
	if err != nil {
		t.Fatalf("%v: %v", key, err)
	}
	return g
}

// TestDragonGoalDefaults: the DragonBaseGoalDefinition, DragonStrafePlayer,
// DragonChargePlayer and DragonFlaming defaults; the BDS move flag (none for
// the death goal).
func TestDragonGoalDefaults(t *testing.T) {
	s := build(t, newStrafePlayer, "minecraft:behavior.dragonstrafeplayer", `{}`).(*strafePlayer)
	if s.active != 1 || s.flight != 0.6 || s.turn != 0.7 || s.zoneMin != 10 || s.zoneMax != 150 ||
		s.switchProb != 0.125 || s.required != 5 || s.rng != 64 || s.viewAngle != 10 {
		t.Errorf("strafe %+v", s)
	}
	c := build(t, newChargePlayer, "minecraft:behavior.dragonchargeplayer", `{}`).(*chargePlayer)
	if c.active != 3 || c.flight != 0.6 || c.threshold != 10 {
		t.Errorf("charge %+v", c)
	}
	f := build(t, newFlaming, "minecraft:behavior.dragonflaming", `{}`).(*flaming)
	if f.count != 4 || f.cooldown != 200 || f.flameTime != 10 || f.roarTime != 40 || f.smokeTime != 200 || f.radius != 5 {
		t.Errorf("flaming %+v", f)
	}
	for key, fac := range map[string]mob.GoalFactory{
		"minecraft:behavior.dragonholdingpattern": newHoldingPattern, "minecraft:behavior.dragontakeoff": newTakeoff,
		"minecraft:behavior.dragonlanding": newLanding, "minecraft:behavior.dragonscanning": newScanning,
		"minecraft:behavior.dragonstrafeplayer": newStrafePlayer, "minecraft:behavior.dragonchargeplayer": newChargePlayer,
		"minecraft:behavior.dragonflaming": newFlaming,
	} {
		if g := build(t, fac, key, `{"priority": 1}`); g.Flags() != mob.FlagMove {
			t.Errorf("%v flags %v, want move", key, g.Flags())
		}
	}
	if g := build(t, newDeath, "minecraft:behavior.dragondeath", `{}`); g.Flags() != 0 {
		t.Errorf("death flags %v", g.Flags())
	}
	s = build(t, newStrafePlayer, "x", `{"active_speed": 2, "target_zone": {"min": 5, "max": 50}, "fireball_range": 32, "view_angle": 20}`).(*strafePlayer)
	if s.active != 2 || s.zoneMin != 5 || s.zoneMax != 50 || s.rng != 32 || s.viewAngle != 20 {
		t.Errorf("strafe from JSON %+v", s)
	}
}

// TestWardenGoalDefaults: SonicBoom, Roar, Dig, Emerge and
// InvestigateSuspiciousLocation definition defaults, and the warden's JSON.
func TestWardenGoalDefaults(t *testing.T) {
	b := build(t, newSonicBoom, "minecraft:behavior.sonic_boom", `{}`).(*sonicBoom)
	if b.duration != 0 || b.speed != 1 || b.damage != 30 || b.cooldown != 100 || b.rangeH != 15 || b.rangeV != 20 ||
		b.untilSound != 34 || b.kbH != 0 || b.kbV != 0 || b.kbCap != 0 || b.Flags() != mob.FlagMove|mob.FlagLook {
		t.Errorf("sonic boom %+v", b)
	}
	b = build(t, newSonicBoom, "minecraft:behavior.sonic_boom", `{"duration": 3, "speed_multiplier": 1.2, "attack_damage": 10, "attack_cooldown": 2, "knockback_vertical_strength": 0.5, "knockback_horizontal_strength": 2.5, "knockback_height_cap": 0.5, "attack_sound": "sonic_boom", "charge_sound": "sonic_charge"}`).(*sonicBoom)
	if b.duration != 60 || b.damage != 10 || b.cooldown != 40 || b.kbH != 2.5 || b.attackSound != "sonic_boom" {
		t.Errorf("warden sonic boom %+v", b)
	}
	if r := build(t, newRoar, "minecraft:behavior.roar", `{"duration": 4.2}`).(*roar); r.duration != 84 {
		t.Errorf("roar %+v", r)
	}
	d := build(t, newDig, "minecraft:behavior.dig", `{}`).(*dig)
	if d.duration != 0 || d.idle != 0 || d.vibration || d.suspicion || d.named || d.daylight {
		t.Errorf("dig %+v", d)
	}
	d = build(t, newDig, "minecraft:behavior.dig", `{"duration": 5.5, "idle_time": 60, "vibration_is_disturbance": true, "suspicion_is_disturbance": true, "on_start": {"event": "on_digging_event"}}`).(*dig)
	if d.duration != 110 || d.idle != 1200 || !d.vibration || !d.suspicion || d.onStart.Event != "on_digging_event" {
		t.Errorf("warden dig %+v", d)
	}
	if e := build(t, newEmerge, "minecraft:behavior.emerge", `{}`).(*emerge); e.duration != 100 || e.cooldown != 10 {
		t.Errorf("emerge %+v", e)
	}
	if i := build(t, newInvestigate, "minecraft:behavior.investigate_suspicious_location", `{}`).(*investigate); i.speed != 1 || i.radius != 1.5 {
		t.Errorf("investigate %+v", i)
	}
}

// TestWitherGoalDefaults: both wither goals use the move flag; the damage
// goal is a target goal.
func TestWitherGoalDefaults(t *testing.T) {
	a := build(t, newWitherAttackPos, "minecraft:behavior.wither_random_attack_pos_goal", `{"priority": 3}`)
	h := build(t, newWitherHighestDamage, "minecraft:behavior.wither_target_highest_damage", `{"priority": 1}`)
	if a.Flags() != mob.FlagMove || h.Flags() != mob.FlagMove {
		t.Errorf("flags %v %v", a.Flags(), h.Flags())
	}
	if tg, ok := h.(mob.TargetGoal); !ok || !tg.IsTargetGoal() {
		t.Errorf("wither_target_highest_damage is not a target goal")
	}
}
