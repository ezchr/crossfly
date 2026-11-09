//go:build crossfly_full

// Package combat holds the hostile mob components: minecraft:shooter,
// explode, burns_in_daylight, environment_sensor, damage_sensor,
// target_nearby_sensor, on_target_acquired, on_target_escape, angry,
// transformation, annotation.break_door and equip_item. See
// README.md in mob/goal/combat for sources and gaps.
package combat

import (
	"encoding/json"
	"fmt"

	"github.com/df-mc/dragonfly/server/mob"
)

func init() {
	mob.RegisterComponent("minecraft:shooter", newShooter)
	mob.RegisterComponent("minecraft:explode", newExplode)
	mob.RegisterComponent("minecraft:burns_in_daylight", newBurnsInDaylight)
	mob.RegisterComponent("minecraft:environment_sensor", newEnvironmentSensor)
	mob.RegisterComponent("minecraft:damage_sensor", newDamageSensor)
	mob.RegisterComponent("minecraft:target_nearby_sensor", newTargetNearbySensor)
	mob.RegisterComponent("minecraft:on_target_acquired", newOnTarget)
	mob.RegisterComponent("minecraft:on_target_escape", newOnTarget)
	mob.RegisterComponent("minecraft:angry", newAngry)
	mob.RegisterComponent("minecraft:transformation", newTransformation)
	mob.RegisterComponent("minecraft:annotation.break_door", newBreakDoor)
	mob.RegisterComponent("minecraft:equip_item", newEquipItem)

	// Everything that fires events runs as a system: a component Ticker
	// that fires an event would rebuild the ticker list it is part of.
	sys := func(name string, order int, f func(m *mob.Mob)) {
		mob.RegisterSystem(mob.System{Name: "combat/" + name, Stage: mob.StageSensors, Order: order, Tick: f})
	}
	sys("explode", 10, func(m *mob.Mob) {
		if e, ok := mob.ComponentOf[*Explode](m); ok {
			e.tick(m)
		}
	})
	sys("environment_sensor", 20, func(m *mob.Mob) {
		if s, ok := mob.ComponentOf[*EnvironmentSensor](m); ok {
			s.run(m)
		}
	})
	sys("target_nearby_sensor", 30, func(m *mob.Mob) {
		if s, ok := mob.ComponentOf[*TargetNearbySensor](m); ok {
			s.run(m)
		}
	})
	sys("angry", 40, func(m *mob.Mob) {
		if a, ok := mob.ComponentOf[*Angry](m); ok {
			a.tick(m)
		}
	})
	sys("burns_in_daylight", 50, burnTick)
	sys("break_door", 60, func(m *mob.Mob) {
		if b, ok := mob.ComponentOf[*BreakDoor](m); ok {
			b.tick(m)
		}
	})
	sys("transformation", 90, func(m *mob.Mob) {
		if t, ok := mob.ComponentOf[*Transformation](m); ok {
			t.tick(m)
		}
	})
}

// EquipItem is minecraft:equip_item: which items the mob may equip
// (excluded_items). behavior.pickup_items does the equipping.
type EquipItem struct {
	Excluded []string
}

func newEquipItem(_ string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		ExcludedItems []json.RawMessage `json:"excluded_items"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("minecraft:equip_item: %w", err)
	}
	e := &EquipItem{}
	for _, r := range v.ExcludedItems {
		var s string
		if json.Unmarshal(r, &s) == nil {
			e.Excluded = append(e.Excluded, s)
			continue
		}
		var o struct{ Item string }
		if json.Unmarshal(r, &o) == nil && o.Item != "" {
			e.Excluded = append(e.Excluded, o.Item)
		}
	}
	return e, nil
}

// Attach ...
func (*EquipItem) Attach(*mob.Mob) {}

// Detach ...
func (*EquipItem) Detach(*mob.Mob) {}
