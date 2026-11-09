//go:build crossfly_full

package control

import (
	"encoding/json"
	"fmt"

	"github.com/df-mc/dragonfly/server/mob"
)

// movementKinds maps the minecraft:movement.* keys to their control kind.
// movement.dolphin is in BDS but no 1.26 definition uses it.
var movementKinds = map[string]Kind{
	"minecraft:movement.basic":      Basic,
	"minecraft:movement.generic":    Generic,
	"minecraft:movement.amphibious": Amphibious,
	"minecraft:movement.sway":       Sway,
	"minecraft:movement.fly":        Fly,
	"minecraft:movement.hover":      Hover,
	"minecraft:movement.glide":      Glide,
	"minecraft:movement.skip":       Skip,
	"minecraft:movement.jump":       Jump,
}

func init() {
	mob.RegisterInitHook(mob.InitHook{Name: "control/defaults", Init: func(m *mob.Mob) {
		m.SetLookControl(NewLookControl())
		m.SetBodyControl(NewBodyControl())
	}})
	for key := range movementKinds {
		mob.RegisterComponent(key, newMovement)
	}
	mob.RegisterComponent("minecraft:jump.static", newJump)
	mob.RegisterComponent("minecraft:jump.dynamic", newJump)
}

func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, v)
}

// movementComponent attaches its MoveControl while it is active.
type movementComponent struct{ c *MoveControl }

func newMovement(key string, raw json.RawMessage) (mob.Component, error) {
	kind, ok := movementKinds[key]
	if !ok {
		return nil, fmt.Errorf("control: unknown movement component %q", key)
	}
	v := struct {
		MaxTurn   *float64        `json:"max_turn"`
		JumpDelay json.RawMessage `json:"jump_delay"`
	}{}
	if err := decode(raw, &v); err != nil {
		return nil, err
	}
	turn := DefaultMaxTurn
	if v.MaxTurn != nil {
		turn = *v.MaxTurn
	}
	c := NewMoveControl(kind, turn)
	if len(v.JumpDelay) != 0 {
		var pair []float64
		var one float64
		switch {
		case json.Unmarshal(v.JumpDelay, &pair) == nil && len(pair) == 2:
			c.jumpDelayMin, c.jumpDelayMax = pair[0], pair[1]
		case json.Unmarshal(v.JumpDelay, &one) == nil:
			c.jumpDelayMin, c.jumpDelayMax = one, one
		}
	}
	return &movementComponent{c: c}, nil
}

func (mc *movementComponent) Attach(m *mob.Mob) { m.SetMoveControl(mc.c) }

func (mc *movementComponent) Detach(m *mob.Mob) {
	if c, ok := m.MoveControl().(*MoveControl); ok && c == mc.c {
		m.SetMoveControl(nil)
	}
}

// jumpComponent attaches its JumpControl while it is active. jump.dynamic's
// skip data (rabbit hop heights) is not modelled: it jumps like jump.static.
type jumpComponent struct{ j *JumpControl }

func newJump(_ string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		JumpPower *float64 `json:"jump_power"`
	}{}
	if err := decode(raw, &v); err != nil {
		return nil, err
	}
	power := 0.0
	if v.JumpPower != nil {
		power = *v.JumpPower
	}
	return &jumpComponent{j: NewJumpControl(power)}, nil
}

func (jc *jumpComponent) Attach(m *mob.Mob) { m.SetJumpControl(jc.j) }

func (jc *jumpComponent) Detach(m *mob.Mob) {
	if j, ok := m.JumpControl().(*JumpControl); ok && j == jc.j {
		m.SetJumpControl(nil)
	}
}
