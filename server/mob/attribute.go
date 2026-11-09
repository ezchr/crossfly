//go:build crossfly_full

package mob

import "math"

// Attribute names (BDS SharedAttributes). Health is not here: it is
// Mob.Health/MaxHealth.
const (
	AttrMovement            = "minecraft:movement"
	AttrUnderwaterMovement  = "minecraft:underwater_movement"
	AttrLavaMovement        = "minecraft:lava_movement"
	AttrFlyingSpeed         = "minecraft:flying_speed"
	AttrFollowRange         = "minecraft:follow_range"
	AttrAttackDamage        = "minecraft:attack_damage"
	AttrKnockbackResistance = "minecraft:knockback_resistance"
	AttrAbsorption          = "minecraft:absorption"
	AttrLuck                = "minecraft:luck"
	AttrJumpStrength        = "minecraft:horse.jump_strength"
)

// Attribute is one attribute instance: a base value clamped to [Min, Max],
// and the current Value (base with modifiers; Phase 0 has no modifiers, so
// Value is Base unless a package sets it).
type Attribute struct {
	Name        string
	Min, Max    float64
	Default     float64
	Base, Value float64
}

// SetBase sets the base and current value (clamped).
func (a *Attribute) SetBase(v float64) {
	v = math.Max(a.Min, math.Min(a.Max, v))
	a.Base, a.Value = v, v
}

// Attributes holds a mob's attributes by name.
type Attributes struct {
	m map[string]*Attribute
}

// attributeDefaults are BDS's SharedAttributes defaults, used until a
// component sets the attribute.
var attributeDefaults = map[string]Attribute{
	AttrMovement:            {Min: 0, Max: math.MaxFloat32, Default: 0.7},
	AttrUnderwaterMovement:  {Min: 0, Max: math.MaxFloat32, Default: 0.02},
	AttrLavaMovement:        {Min: 0, Max: math.MaxFloat32, Default: 0.02},
	AttrFlyingSpeed:         {Min: 0, Max: math.MaxFloat32, Default: 0.02},
	AttrFollowRange:         {Min: 0, Max: 2048, Default: 16},
	AttrAttackDamage:        {Min: 0, Max: math.MaxFloat32, Default: 1},
	AttrKnockbackResistance: {Min: 0, Max: 1, Default: 0},
	AttrAbsorption:          {Min: 0, Max: math.MaxFloat32, Default: 0},
	AttrLuck:                {Min: -1024, Max: 1024, Default: 0},
	AttrJumpStrength:        {Min: 0, Max: 2, Default: 0.7},
}

func newAttributes() *Attributes { return &Attributes{m: map[string]*Attribute{}} }

// Get returns the attribute, creating it from its default (or 0 with no
// bounds for unknown names).
func (a *Attributes) Get(name string) *Attribute {
	if at, ok := a.m[name]; ok {
		return at
	}
	d, ok := attributeDefaults[name]
	if !ok {
		d = Attribute{Min: -math.MaxFloat32, Max: math.MaxFloat32}
	}
	d.Name, d.Base, d.Value = name, d.Default, d.Default
	at := &d
	a.m[name] = at
	return at
}

// Value returns the current value of an attribute.
func (a *Attributes) Value(name string) float64 { return a.Get(name).Value }

// SetBase sets an attribute's base value.
func (a *Attributes) SetBase(name string, v float64) { a.Get(name).SetBase(v) }

// SetRange sets an attribute's bounds and clamps it.
func (a *Attributes) SetRange(name string, min, max float64) {
	at := a.Get(name)
	at.Min, at.Max = min, max
	at.SetBase(at.Base)
}

// All returns every attribute that was used.
func (a *Attributes) All() []*Attribute {
	out := make([]*Attribute, 0, len(a.m))
	for _, at := range a.m {
		out = append(out, at)
	}
	return out
}
