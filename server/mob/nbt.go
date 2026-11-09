//go:build crossfly_full

package mob

import (
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
)

// NBT layout of a mob (besides the world's own Pos/Motion/Rotation/Fire/...):
//
//	Health           float32
//	HeadYaw, BodyYaw float32
//	Persistent       byte
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
	return out
}

// decodeNBT restores the core fields and the components' saved state, after
// the components attached.
func (m *Mob) decodeNBT(d map[string]any) {
	s := m.s
	if h := nbtconv.Float32(d, "Health"); h > 0 {
		s.health.AddHealth(float64(h) - s.health.Health())
	}
	if _, ok := d["HeadYaw"]; ok {
		s.headYaw = float64(nbtconv.Float32(d, "HeadYaw"))
		s.bodyYaw = float64(nbtconv.Float32(d, "BodyYaw"))
	}
	s.persistent = nbtconv.Uint8(d, "Persistent") == 1
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
}

func boolByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}
