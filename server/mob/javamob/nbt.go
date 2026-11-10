//go:build crossfly_full

package javamob

import (
	"github.com/df-mc/dragonfly/server/mob"
)

// EncodeNBT implements mob.Persister: Mob.addAdditionalSaveData (attributes with their permanent modifiers,
// persistence, drop chances, hand) and the behaviour's own fields. It runs outside any transaction.
func (s *state) EncodeNBT() map[string]any {
	attrs := map[string]any{}
	for k, a := range s.attrs.m {
		e := map[string]any{"base": a.base}
		var mods []any
		for _, mod := range a.modifiers {
			if mod.Transient {
				continue
			}
			mods = append(mods, map[string]any{"id": mod.ID, "amount": mod.Amount, "operation": int32(mod.Op)})
		}
		if len(mods) > 0 {
			e["modifiers"] = mods
		}
		attrs[string(k)] = e
	}
	chances := make([]any, len(s.dropChances))
	for i, c := range s.dropChances {
		chances[i] = float32(c)
	}
	data := map[string]any{
		"attributes":          attrs,
		"PersistenceRequired": s.persistenceRequired,
		"CanPickUpLoot":       s.canPickUpLoot,
		"LeftHanded":          s.leftHanded,
		"NoAI":                s.noAI,
		"drop_chances":        chances,
		"FallDistance":        s.fallDistance,
		"finalized":           s.finalized,
		"DeathLootTable":      s.lootTable,
	}
	if p, ok := s.b.(Persister); ok {
		own := map[string]any{}
		p.EncodeNBT(own)
		data["behaviour"] = own
	}
	return data
}

// DecodeNBT implements mob.Persister.
func (s *state) DecodeNBT(bm *mob.Mob, data map[string]any) {
	m, ok := wrap(bm)
	if !ok {
		return
	}
	if attrs, ok := data["attributes"].(map[string]any); ok {
		for k, v := range attrs {
			e, _ := v.(map[string]any)
			a := s.attrs.Instance(Attribute(k))
			if b, ok := e["base"].(float64); ok {
				a.SetBase(b)
			}
			mods, _ := e["modifiers"].([]any)
			for _, raw := range mods {
				md, _ := raw.(map[string]any)
				id, _ := md["id"].(string)
				amount, _ := md["amount"].(float64)
				op, _ := md["operation"].(int32)
				a.AddModifier(Modifier{ID: id, Amount: amount, Op: ModifierOp(op)})
			}
		}
	}
	s.persistenceRequired, _ = data["PersistenceRequired"].(bool)
	s.canPickUpLoot, _ = data["CanPickUpLoot"].(bool)
	s.leftHanded, _ = data["LeftHanded"].(bool)
	s.noAI, _ = data["NoAI"].(bool)
	s.fallDistance, _ = data["FallDistance"].(float64)
	s.finalized, _ = data["finalized"].(bool)
	if lt, ok := data["DeathLootTable"].(string); ok {
		s.lootTable = lt
	}
	if c, ok := data["drop_chances"].([]any); ok {
		for i := 0; i < len(c) && i < len(s.dropChances); i++ {
			if f, ok := c[i].(float32); ok {
				s.dropChances[i] = float64(f)
			}
		}
	}
	if s.persistenceRequired {
		bm.SetPersistent(true)
	}
	if p, ok := s.b.(Persister); ok {
		if own, ok := data["behaviour"].(map[string]any); ok {
			p.DecodeNBT(m, own)
		}
	}
	m.Mob.SetSpeed(m.Attr(AttrMovementSpeed))
}
