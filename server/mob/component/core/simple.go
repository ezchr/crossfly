//go:build crossfly_full

package core

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
)

// typeFamily is minecraft:type_family {"family": [...]}.
type typeFamily struct {
	families []string
	prev     []string
}

func newTypeFamily(key string, raw json.RawMessage) (mob.Component, error) {
	var j struct {
		Family json.RawMessage `json:"family"`
	}
	if err := decode(raw, &j); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	fams, err := stringList(j.Family)
	if err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	return &typeFamily{families: fams}, nil
}

func (c *typeFamily) Attach(m *mob.Mob) {
	c.prev = m.Families()
	m.SetFamilies(c.families)
}
func (c *typeFamily) Detach(m *mob.Mob) { m.SetFamilies(c.prev) }

// collisionBox is minecraft:collision_box {"width", "height"}.
type collisionBox struct {
	W, H   float64
	pw, ph float64
}

func newCollisionBox(key string, raw json.RawMessage) (mob.Component, error) {
	j := struct {
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	}{Width: 1, Height: 1}
	if err := decode(raw, &j); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	return &collisionBox{W: j.Width, H: j.Height}, nil
}

func (c *collisionBox) Attach(m *mob.Mob) {
	meta := m.Meta()
	c.pw, c.ph = meta.Width, meta.Height
	m.UpdateMeta(func(meta *mob.Meta) { meta.Width, meta.Height = c.W, c.H })
}
func (c *collisionBox) Detach(m *mob.Mob) {
	m.UpdateMeta(func(meta *mob.Meta) { meta.Width, meta.Height = c.pw, c.ph })
}

// scale is minecraft:scale {"value"}.
type scale struct{ v float64 }

func newScale(key string, raw json.RawMessage) (mob.Component, error) {
	j := struct {
		Value float64 `json:"value"`
	}{Value: 1}
	if err := decode(raw, &j); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	return &scale{v: j.Value}, nil
}

func (c *scale) Attach(m *mob.Mob) { m.UpdateMeta(func(meta *mob.Meta) { meta.Scale = c.v }) }
func (c *scale) Detach(m *mob.Mob) { m.UpdateMeta(func(meta *mob.Meta) { meta.Scale = 1 }) }

// flag is a component whose presence sets a client flag (is_baby,
// is_sheared, is_saddled, ...).
type flag struct{ set func(meta *mob.Meta, v bool) }

func newFlag(set func(meta *mob.Meta, v bool)) mob.ComponentFactory {
	return func(string, json.RawMessage) (mob.Component, error) { return &flag{set: set}, nil }
}

func (c *flag) Attach(m *mob.Mob) { m.UpdateMeta(func(meta *mob.Meta) { c.set(meta, true) }) }
func (c *flag) Detach(m *mob.Mob) { m.UpdateMeta(func(meta *mob.Meta) { c.set(meta, false) }) }

// intValue is a component {"value": n} copied into a client field
// (variant, mark_variant, skin_id, color, color2). The value may be changed
// at run time (breeding mixes sheep colours, dyeing), and the changed value
// is saved with the mob.
type intValue struct {
	key string
	v   int32
	cur int32
	set func(meta *mob.Meta, v int32)
}

func newIntValue(set func(meta *mob.Meta, v int32)) mob.ComponentFactory {
	return func(key string, raw json.RawMessage) (mob.Component, error) {
		var j struct {
			Value int32 `json:"value"`
		}
		if err := decode(raw, &j); err != nil {
			return nil, fmt.Errorf("%v: %w", key, err)
		}
		return &intValue{key: key, v: j.Value, cur: j.Value, set: set}, nil
	}
}

func (c *intValue) Attach(m *mob.Mob) { m.UpdateMeta(func(meta *mob.Meta) { c.set(meta, c.cur) }) }
func (c *intValue) Detach(m *mob.Mob) { m.UpdateMeta(func(meta *mob.Meta) { c.set(meta, 0) }) }

func (c *intValue) EncodeNBT() map[string]any {
	if c.cur == c.v {
		return nil
	}
	return map[string]any{"Value": c.cur}
}

func (c *intValue) DecodeNBT(m *mob.Mob, d map[string]any) {
	if v, ok := d["Value"].(int32); ok {
		c.cur = v
		m.UpdateMeta(func(meta *mob.Meta) { c.set(meta, v) })
	}
}

// setIntValue changes the value of the active component under key; without
// one it only changes the client field.
func setIntValue(m *mob.Mob, key string, v int32, set func(meta *mob.Meta, v int32)) {
	if c, ok := m.Component(key); ok {
		if iv, ok := c.(*intValue); ok {
			iv.cur = v
		}
	}
	m.UpdateMeta(func(meta *mob.Meta) { set(meta, v) })
}

func setColor(meta *mob.Meta, v int32)       { meta.Color = uint8(v) }
func setColor2(meta *mob.Meta, v int32)      { meta.Color2 = uint8(v) }
func setVariant(meta *mob.Meta, v int32)     { meta.Variant = v }
func setMarkVariant(meta *mob.Meta, v int32) { meta.MarkVariant = v }
func setSkinID(meta *mob.Meta, v int32)      { meta.SkinID = v }

// SetColor changes a mob's minecraft:color (sheep wool colour) for good.
func SetColor(m *mob.Mob, c uint8) { setIntValue(m, "minecraft:color", int32(c), setColor) }

// SetVariant changes a mob's minecraft:variant for good.
func SetVariant(m *mob.Mob, v int32) { setIntValue(m, "minecraft:variant", v, setVariant) }

// SetMarkVariant changes a mob's minecraft:mark_variant for good.
func SetMarkVariant(m *mob.Mob, v int32) { setIntValue(m, "minecraft:mark_variant", v, setMarkVariant) }

// nop is a component that has nothing to do on the server (nameable is read
// by the core through its key, is_hidden_when_invisible is client-side) or is
// a stub (leashable_to, balloonable).
type nop struct{}

func newNop(string, json.RawMessage) (mob.Component, error) { return nop{}, nil }

func (nop) Attach(*mob.Mob) {}
func (nop) Detach(*mob.Mob) {}

func stringList(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []string{s}, nil
	}
	var l []string
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, err
	}
	return l, nil
}

func secondsToDuration(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }

func ticksOf(seconds float64) int { return int(seconds*20 + 0.5) }

var effectsByName = map[string]effect.LastingType{
	"poison":          effect.Poison,
	"fatal_poison":    effect.FatalPoison,
	"wither":          effect.Wither,
	"hunger":          effect.Hunger,
	"slowness":        effect.Slowness,
	"weakness":        effect.Weakness,
	"nausea":          effect.Nausea,
	"blindness":       effect.Blindness,
	"mining_fatigue":  effect.MiningFatigue,
	"levitation":      effect.Levitation,
	"darkness":        effect.Darkness,
	"speed":           effect.Speed,
	"regeneration":    effect.Regeneration,
	"fire_resistance": effect.FireResistance,
	"resistance":      effect.Resistance,
	"strength":        effect.Strength,
	"invisibility":    effect.Invisibility,
	"water_breathing": effect.WaterBreathing,
}

func effectByName(name string) (effect.LastingType, bool) {
	t, ok := effectsByName[name]
	return t, ok
}
