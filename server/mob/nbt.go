//go:build crossfly_full

package mob

import (
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// NBT layout of a mob (besides the world's own Pos/Motion/Rotation/Fire/...):
//
//	Health           float32
//	MaxHealth        float32
//	Attributes       {name: base float32} of the attributes used
//	Variant, MarkVariant int32; Color, Color2 byte (Actor::addAdditionalSaveData)
//	Strength, StrengthMax int32 (llamas; absent when unset)
//	HeadYaw, BodyYaw float32
//	Persistent       byte
//	IsTamed          byte (the TAMED actor flag, Actor::addSaveData)
//	HomePos          [3]int32 (absent without a home)
//	Mainhand, Offhand, BodyArmor item NBT
//	Armor            item list
//	mob:definition   Instance.EncodeNBT (groups, properties; package mob/def)
//	mob:components   map of component key -> Persister.EncodeNBT
func (s *state) encodeNBT() map[string]any {
	out := map[string]any{
		"Health":     float32(s.health.Health()),
		"HeadYaw":    float32(s.headYaw),
		"BodyYaw":    float32(s.bodyYaw),
		"Persistent": boolByte(s.persistent),
		"IsTamed":    boolByte(s.meta.Tamed),
		"MaxHealth":  float32(s.health.MaxHealth()),
		// The variant data BDS keeps on the actor (bred babies get theirs
		// from their parents, without a minecraft:variant component).
		"Variant":     s.meta.Variant,
		"MarkVariant": s.meta.MarkVariant,
		"Color":       s.meta.Color,
		"Color2":      s.meta.Color2,
	}
	// Rolled (minecraft:movement ranges) or bred (attribute blending)
	// attributes keep their value.
	if attrs := s.attrs.All(); len(attrs) > 0 {
		am := make(map[string]any, len(attrs))
		for _, a := range attrs {
			am[a.Name] = float32(a.Base)
		}
		out["Attributes"] = am
	}
	d, _ := s.scratch[actorDataKey{}].(map[uint32]any)
	if v, ok := d[protocol.EntityDataKeyStrength].(int32); ok {
		out["Strength"] = v
	}
	if v, ok := d[protocol.EntityDataKeyStrengthMax].(int32); ok {
		out["StrengthMax"] = v
	}
	if s.home != nil {
		out["HomePos"] = nbtconv.PosToInt32Slice(*s.home)
	}
	if !s.mainHand.Empty() {
		out["Mainhand"] = item.WriteNBT(s.mainHand, true)
	}
	if !s.offHand.Empty() {
		out["Offhand"] = item.WriteNBT(s.offHand, true)
	}
	if !s.bodyArmour.Empty() {
		out["BodyArmor"] = item.WriteNBT(s.bodyArmour, true)
	}
	if s.armour != nil {
		if inv := nbtconv.InvToNBT(s.armour.Inventory()); len(inv) > 0 {
			out["Armor"] = inv
		}
	}
	s.encodeOwner(out)
	if s.inst != nil {
		if d := s.inst.EncodeNBT(); len(d) > 0 {
			out["mob:definition"] = d
		}
	}
	comps := map[string]any{}
	for _, c := range s.comps {
		if p, ok := c.comp.(Persister); ok {
			if d := p.EncodeNBT(); len(d) > 0 {
				comps[c.key] = d
			}
		}
	}
	if len(comps) > 0 {
		out["mob:components"] = comps
	}
	s.encodeNBTHooks(out)
	return out
}

// decodeNBT restores the core fields and the components' saved state, after
// the components attached.
func (m *Mob) decodeNBT(d map[string]any) {
	s := m.s
	if mh := nbtconv.Float32(d, "MaxHealth"); mh > 0 {
		s.health.SetMaxHealth(float64(mh))
	}
	if am, ok := d["Attributes"].(map[string]any); ok {
		for name, v := range am {
			if f, ok := v.(float32); ok {
				a := s.attrs.Get(name)
				if float64(f) > a.Max {
					a.Max = float64(f)
				}
				a.SetBase(float64(f))
			}
		}
	}
	if _, ok := d["Variant"]; ok {
		s.meta.Variant = nbtconv.Int32(d, "Variant")
		s.meta.MarkVariant = nbtconv.Int32(d, "MarkVariant")
		s.meta.Color = nbtconv.Uint8(d, "Color")
		s.meta.Color2 = nbtconv.Uint8(d, "Color2")
	}
	if v, ok := d["StrengthMax"].(int32); ok {
		m.SetActorData(protocol.EntityDataKeyStrengthMax, v)
	}
	if v, ok := d["Strength"].(int32); ok {
		m.SetActorData(protocol.EntityDataKeyStrength, v)
	}
	if h := nbtconv.Float32(d, "Health"); h > 0 {
		s.health.AddHealth(float64(h) - s.health.Health())
	}
	if _, ok := d["HeadYaw"]; ok {
		s.headYaw = float64(nbtconv.Float32(d, "HeadYaw"))
		s.bodyYaw = float64(nbtconv.Float32(d, "BodyYaw"))
	}
	s.persistent = nbtconv.Uint8(d, "Persistent") == 1
	// The TAMED flag (BDS Actor::readAdditionalSaveData "IsTamed"): taming and
	// breeding set it without minecraft:is_tamed. A minecraft:is_tamed
	// attached from the saved groups has set it already.
	if nbtconv.Uint8(d, "IsTamed") == 1 && !s.meta.Tamed {
		m.UpdateMeta(func(meta *Meta) { meta.Tamed = true })
	}
	if _, ok := d["HomePos"]; ok {
		p := nbtconv.Pos(d, "HomePos")
		s.home = &p
	}
	if v, ok := d["Mainhand"].(map[string]any); ok {
		s.mainHand = item.ReadNBT(v, nil)
	}
	if v, ok := d["Offhand"].(map[string]any); ok {
		s.offHand = item.ReadNBT(v, nil)
	}
	if v, ok := d["BodyArmor"].(map[string]any); ok {
		s.bodyArmour = item.ReadNBT(v, nil)
	}
	if v := nbtconv.Slice(d, "Armor"); len(v) > 0 {
		nbtconv.InvFromNBT(m.Armour().Inventory(), v)
	}
	s.decodeOwner(d)
	if comps, ok := d["mob:components"].(map[string]any); ok {
		for _, c := range s.comps {
			if p, ok := c.comp.(Persister); ok {
				if cd, ok := comps[c.key].(map[string]any); ok {
					p.DecodeNBT(m, cd)
				}
			}
		}
	}
	m.decodeNBTHooks(d)
}

func boolByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}
