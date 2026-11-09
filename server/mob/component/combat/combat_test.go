//go:build crossfly_full

package combat

import (
	"encoding/json"
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/world"
)

// Factories accept the vanilla 1.26 JSON of the Phase 1 hostiles.
func TestFactoriesParseVanilla(t *testing.T) {
	cases := map[string]string{
		"minecraft:shooter":               `{"def": "minecraft:arrow", "sound": "bow"}`,
		"minecraft:explode":               `{"causes_fire": false, "fuse_lit": true, "power": 3, "destroy_affected_by_griefing": true, "fuse_length": 1.5}`,
		"minecraft:burns_in_daylight":     `{}`,
		"minecraft:environment_sensor":    `{"triggers": {"event": "minecraft:become_neutral", "filters": {"operator": ">", "test": "is_brightness", "value": 0.49}}}`,
		"minecraft:damage_sensor":         `{"triggers": {"deals_damage": "no", "on_damage": {"event": "minecraft:become_charged", "filters": {"subject": "other", "test": "is_family", "value": "lightning"}}}}`,
		"minecraft:target_nearby_sensor":  `{"inside_range": 2.5, "on_inside_range": {"event": "minecraft:start_exploding", "target": "self"}, "must_see": true, "outside_range": 6}`,
		"minecraft:on_target_acquired":    `{"event": "minecraft:become_angry"}`,
		"minecraft:on_target_escape":      `{}`,
		"minecraft:angry":                 `{"calm_event": {"event": "minecraft:become_calm", "target": "self"}, "duration": 10, "duration_delta": 3}`,
		"minecraft:transformation":        `{"delay": {"value": 15}, "preserve_equipment": true, "into": "minecraft:drowned<minecraft:as_adult>", "transformation_sound": "convert_to_drowned"}`,
		"minecraft:annotation.break_door": `{}`,
		"minecraft:equip_item":            `{"excluded_items": [{"item": "minecraft:banner:15"}]}`,
	}
	for key, js := range cases {
		f := componentFactory(key)
		if f == nil {
			t.Fatalf("%v not registered", key)
		}
		if _, err := f(key, json.RawMessage(js)); err != nil {
			t.Errorf("%v: %v", key, err)
		}
	}
}

func componentFactory(key string) mob.ComponentFactory {
	switch key {
	case "minecraft:shooter":
		return newShooter
	case "minecraft:explode":
		return newExplode
	case "minecraft:burns_in_daylight":
		return newBurnsInDaylight
	case "minecraft:environment_sensor":
		return newEnvironmentSensor
	case "minecraft:damage_sensor":
		return newDamageSensor
	case "minecraft:target_nearby_sensor":
		return newTargetNearbySensor
	case "minecraft:on_target_acquired", "minecraft:on_target_escape":
		return newOnTarget
	case "minecraft:angry":
		return newAngry
	case "minecraft:transformation":
		return newTransformation
	case "minecraft:annotation.break_door":
		return newBreakDoor
	case "minecraft:equip_item":
		return newEquipItem
	}
	return nil
}

func TestRegistered(t *testing.T) {
	for _, k := range []string{"minecraft:shooter", "minecraft:explode", "minecraft:angry", "minecraft:transformation", "minecraft:annotation.break_door"} {
		if !mob.Supported(k) {
			t.Errorf("%v not registered", k)
		}
	}
}

func TestParseRange(t *testing.T) {
	for js, want := range map[string]sense.Range{
		`3`:                                  {Min: 3, Max: 3},
		`[1, 2]`:                             {Min: 1, Max: 2},
		`{"min": 3.0, "max": 3.0}`:           {Min: 3, Max: 3},
		`{"min": 5}`:                         {Min: 5, Max: 5},
		`{"range_min": 0.5, "range_max": 2}`: {Min: 0.5, Max: 2},
		`{"value": 15}`:                      {Min: 15, Max: 15},
	} {
		got, err := sense.ParseRange(json.RawMessage(js), sense.Range{})
		if err != nil || got != want {
			t.Errorf("%s: got %v %v, want %v", js, got, err, want)
		}
	}
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 50; i++ {
		if v := (sense.Range{Min: 7, Max: 13}).RollInt(r); v < 7 || v > 13 {
			t.Fatalf("RollInt %v out of [7,13]", v)
		}
	}
}

func TestParseTriggers(t *testing.T) {
	ts, err := sense.ParseTriggers(json.RawMessage(`[{"event": "a"}, {"event": "b", "target": "target"}, {}]`))
	if err != nil || len(ts) != 2 || ts[0].Target != "self" || ts[1].Target != "target" {
		t.Fatalf("got %+v %v", ts, err)
	}
	one, err := sense.ParseTriggers(json.RawMessage(`{"event": "x", "filters": {"test": "is_daytime"}}`))
	if err != nil || len(one) != 1 || one[0].Event != "x" {
		t.Fatalf("got %+v %v", one, err)
	}
}

func TestSplitIdentifier(t *testing.T) {
	id, ev := SplitIdentifier("minecraft:drowned<minecraft:as_adult>")
	if id != "minecraft:drowned" || ev != "minecraft:as_adult" {
		t.Fatalf("got %q %q", id, ev)
	}
	if id, ev := SplitIdentifier("minecraft:stray"); id != "minecraft:stray" || ev != "" {
		t.Fatalf("got %q %q", id, ev)
	}
}

func TestDamageCause(t *testing.T) {
	for src, want := range map[world.DamageSource]string{
		entity.AttackDamageSource{}:                                         "entity_attack",
		entity.ProjectileDamageSource{}:                                     "projectile",
		entity.FallDamageSource{}:                                           "fall",
		entity.LightningDamageSource{}:                                      "lightning",
		block.FireDamageSource{}:                                            "fire",
		block.LavaDamageSource{}:                                            "lava",
		entity.ExplosionDamageSource{Source: world.EntityExplosionSource{}}: "entity_explosion",
	} {
		if got := sense.DamageCause(src); got != want {
			t.Errorf("%T: %q, want %q", src, got, want)
		}
	}
}

func TestBoxDistance(t *testing.T) {
	a := cube.Box(0, 0, 0, 1, 1, 1)
	if d := sense.BoxDistance(a, cube.Box(3, 0, 0, 4, 1, 1)); d != 2 {
		t.Fatalf("gap 2 measured %v", d)
	}
	if d := sense.BoxDistance(a, cube.Box(0.5, 0.5, 0.5, 2, 2, 2)); d != 0 {
		t.Fatalf("overlap measured %v", d)
	}
}

func TestBrightnessRamp(t *testing.T) {
	// In float32 light 12 lands just above 0.5, so the daylight burn starts at 12.
	f := sense.BrightnessOf
	if f(11) > 0.5 || f(12) <= 0.5 || f(15) != 1 {
		t.Fatalf("ramp: 11=%v 12=%v 15=%v", f(11), f(12), f(15))
	}
}
