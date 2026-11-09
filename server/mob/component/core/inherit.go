//go:build crossfly_full

package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
)

// inheritance is what a bred baby takes from its parents, as
// BreedableComponent::_handleMate does after the baby is born: attributes
// (blended), variant, mark variant and colour (from a random parent unless
// they mutate), deny_parents_variant and strength. The keys are in
// minecraft:offspring (1.26) or, in 1.21 files, minecraft:breedable:
//
//	blend_attributes (default true; false skips all of it: panda, sulfur cube)
//	parent_centric_attribute_blending [attribute names]
//	mutation_factor {variant, extra_variant, color} (default 0)
//	mutation_strategy "none" (default: a mutated value stays the baby's own) or "random"
//	random_variant_mutation_interval, random_extra_variant_mutation_interval [min, max]
//	deny_parents_variant {chance, min_variant, max_variant}
type inheritance struct {
	blend                   bool
	parentCentric           []string
	mutVariant, mutExtra    float64
	mutColour               float64
	randomStrategy          bool
	variantRange, markRange [2]int32
	deny                    *denyVariant
}

type denyVariant struct {
	chance   float64
	min, max int32
}

func parseInheritance(raw json.RawMessage) (inheritance, error) {
	j := struct {
		Blend         *bool    `json:"blend_attributes"`
		ParentCentric []string `json:"parent_centric_attribute_blending"`
		Mutation      struct {
			Variant      float64 `json:"variant"`
			ExtraVariant float64 `json:"extra_variant"`
			Color        float64 `json:"color"`
		} `json:"mutation_factor"`
		Strategy     string  `json:"mutation_strategy"`
		VariantRange []int32 `json:"random_variant_mutation_interval"`
		MarkRange    []int32 `json:"random_extra_variant_mutation_interval"`
		Deny         *struct {
			Chance float64 `json:"chance"`
			Min    int32   `json:"min_variant"`
			Max    int32   `json:"max_variant"`
		} `json:"deny_parents_variant"`
	}{}
	if err := decode(raw, &j); err != nil {
		return inheritance{}, err
	}
	in := inheritance{blend: true, parentCentric: j.ParentCentric,
		mutVariant: j.Mutation.Variant, mutExtra: j.Mutation.ExtraVariant, mutColour: j.Mutation.Color,
		randomStrategy: strings.EqualFold(j.Strategy, "random")}
	if j.Blend != nil {
		in.blend = *j.Blend
	}
	toRange := func(v []int32) [2]int32 {
		switch len(v) {
		case 0:
			return [2]int32{}
		case 1:
			return [2]int32{v[0], v[0]}
		}
		return [2]int32{min(v[0], v[1]), max(v[0], v[1])}
	}
	in.variantRange, in.markRange = toRange(j.VariantRange), toRange(j.MarkRange)
	if j.Deny != nil {
		in.deny = &denyVariant{chance: j.Deny.Chance, min: j.Deny.Min, max: j.Deny.Max}
	}
	return in, nil
}

// blendedAttributes are the attributes _handleMate blends that this server
// models per mob.
var blendedAttributes = [...]string{"minecraft:health", mob.AttrMovement, mob.AttrJumpStrength}

// apply gives baby what it inherits from m (the parent whose breedable made
// it) and partner.
func (in inheritance) apply(m, partner, baby *mob.Mob) {
	r := m.Rand()
	for _, name := range blendedAttributes {
		in.blendAttribute(m, partner, baby, name)
	}
	pick := func() *mob.Mob {
		if r.IntN(2) == 0 {
			return partner
		}
		return m
	}
	// Variant: a random parent's, unless it mutates (then the baby keeps the
	// one its born event gave it, or rolls one with the random strategy).
	if r.Float64() <= in.mutVariant {
		if in.randomStrategy {
			SetVariant(baby, rollRange(m, in.variantRange))
		}
	} else {
		SetVariant(baby, pick().Meta().Variant)
	}
	if r.Float64() <= in.mutExtra {
		if in.randomStrategy {
			SetMarkVariant(baby, rollRange(m, in.markRange))
		}
	} else {
		SetMarkVariant(baby, pick().Meta().MarkVariant)
	}
	if r.Float64() > in.mutColour {
		SetColor(baby, pick().Meta().Color)
	}
	// deny_parents_variant: parents of one variant may have a baby of another
	// (two red mooshrooms, 1/1024: a brown one).
	if d := in.deny; d != nil && m.Meta().Variant == partner.Meta().Variant && r.Float64() < d.chance {
		var choices []int32
		for v := d.min; v <= d.max; v++ {
			if v != m.Meta().Variant {
				choices = append(choices, v)
			}
		}
		if len(choices) > 0 {
			SetVariant(baby, choices[r.IntN(len(choices))])
		}
	}
	// Strength (llamas): a random parent's maximum and strength, +1 at 3%.
	from := pick()
	if HasStrength(from) {
		SetStrengthMax(baby, StrengthMax(from))
		s := Strength(from)
		if r.Float64() < 0.03 {
			s++
		}
		SetStrength(baby, s)
	}
}

func rollRange(m *mob.Mob, rg [2]int32) int32 {
	if rg[1] <= rg[0] {
		return rg[0]
	}
	return rg[0] + int32(m.Rand().IntN(int(rg[1]-rg[0])+1))
}

// attrValue returns the value of an attribute of m (health: the maximum).
func attrValue(m *mob.Mob, name string) float64 {
	if name == "minecraft:health" {
		return m.MaxHealth()
	}
	return m.Attributes().Get(name).Base
}

func setAttrValue(m *mob.Mob, name string, v float64) {
	if name == "minecraft:health" {
		v = math.Max(1, math.Round(v))
		m.SetMaxHealth(v)
		m.SetHealth(v)
		return
	}
	a := m.Attributes().Get(name)
	if v > a.Max {
		m.Attributes().SetRange(name, a.Min, v)
	}
	a.SetBase(v)
}

// blendAttribute is the attribute loop of _handleMate: an attribute named in
// parent_centric_attribute_blending blends around the parents within the
// range of the baby's definition (setOffspringAttributesWithParentCentricBlending);
// any other is the mean of the baby's and the parents' values.
func (in inheritance) blendAttribute(m, partner, baby *mob.Mob, name string) {
	if name != "minecraft:health" && !baby.HasComponent(name) {
		return
	}
	for _, pc := range in.parentCentric {
		if normaliseID(pc) == name {
			lo, hi := attrValue(baby, name), attrValue(baby, name)
			if raw, ok := baby.ComponentJSON(name); ok {
				var j struct {
					Value rangeValue `json:"value"`
				}
				if decode(raw, &j) == nil && j.Value.Set {
					lo, hi = j.Value.Min, j.Value.Max
				}
			}
			setAttrValue(baby, name, parentCentricBlend(m, attrValue(m, name), attrValue(partner, name), lo, hi))
			return
		}
	}
	if attrValue(m, name) == attrValue(baby, name) && attrValue(partner, name) == attrValue(baby, name) {
		return
	}
	setAttrValue(baby, name, (attrValue(baby, name)+attrValue(m, name)+attrValue(partner, name))/3)
}

// parentCentricBlend is BreedableComponent::setOffspringAttributesWithParentCentricBlending:
// the mean of the parents (clamped to [lo, hi]) plus a triangular spread of
// their difference and 30% of the range, mirrored back into the range.
func parentCentricBlend(m *mob.Mob, a, b, lo, hi float64) float64 {
	if lo > hi {
		lo, hi = hi, lo
	}
	if hi-lo < 1.1920929e-7 {
		return lo
	}
	a, b = math.Max(lo, math.Min(hi, a)), math.Max(lo, math.Min(hi, b))
	r := m.Rand()
	t := (r.Float64()+r.Float64()+r.Float64())/3 - 0.5
	v := t*(math.Abs(a-b)+(hi-lo)*0.3) + (a+b)*0.5
	switch {
	case v > hi:
		v = hi - (v - hi)
	case v < lo:
		v = lo + (lo - v)
	}
	return v
}

// assignTrust makes a baby that trusts players (fox) trust the players who
// put its parents in love (_handleMate -> TrustComponent::assignTrustedPlayer).
func (b *Breedable) assignTrust(m, partner, baby *mob.Mob) {
	t, ok := mob.ComponentOf[interface{ AddTrusted(world.Entity) }](baby)
	if !ok {
		return
	}
	if e, ok := b.LoveCause(m); ok {
		t.AddTrusted(e)
	}
	if pb, ok := BreedableOf(partner); ok {
		if e, ok := pb.LoveCause(partner); ok {
			t.AddTrusted(e)
		}
	}
}

// envRequirement is one entry of environment_requirements: at least count
// of blocks within radius (a cube) of the mob.
type envRequirement struct {
	blocks []string
	count  int
	radius int
}

func parseEnvRequirements(raw json.RawMessage) ([]envRequirement, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	type entry struct {
		Blocks json.RawMessage `json:"blocks"`
		Count  int             `json:"count"`
		Radius float64         `json:"radius"`
	}
	var list []entry
	if raw[0] == '[' {
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, err
		}
	} else {
		var e entry
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		list = []entry{e}
	}
	var out []envRequirement
	for _, e := range list {
		req := envRequirement{count: e.Count, radius: int(e.Radius)}
		var names []string
		var one string
		if json.Unmarshal(e.Blocks, &one) == nil {
			names = []string{one}
		} else if err := json.Unmarshal(e.Blocks, &names); err != nil {
			var objs []struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(e.Blocks, &objs); err != nil {
				return nil, fmt.Errorf("blocks: %s", e.Blocks)
			}
			for _, o := range objs {
				names = append(names, o.Name)
			}
		}
		for _, n := range names {
			req.blocks = append(req.blocks, normaliseID(n))
		}
		out = append(out, req)
	}
	return out, nil
}

// meetsEnvironment is BreedableComponent::_meetsEnvironmentRequirements.
func (b *Breedable) meetsEnvironment(m *mob.Mob) bool {
	if len(b.environment) == 0 {
		return true
	}
	tx := m.Tx()
	p := m.Position()
	for _, req := range b.environment {
		r := float64(req.radius)
		lo := cube.PosFromVec3(p.Sub([3]float64{r, r, r}))
		hi := cube.PosFromVec3(p.Add([3]float64{r, r, r}))
		n := 0
	count:
		for x := lo[0]; x <= hi[0]; x++ {
			for y := lo[1]; y <= hi[1]; y++ {
				for z := lo[2]; z <= hi[2]; z++ {
					name, _ := tx.Block(cube.Pos{x, y, z}).EncodeBlock()
					for _, want := range req.blocks {
						if name == want {
							if n++; n >= req.count {
								break count
							}
						}
					}
				}
			}
		}
		if n < req.count {
			return false
		}
	}
	return true
}
