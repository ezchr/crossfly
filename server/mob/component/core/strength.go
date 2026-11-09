//go:build crossfly_full

package core

import (
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// A mob's strength (llamas: 3 chest slots per point) is actor data, as in BDS
// (Actor::getStrength/setStrength, getStrengthMax/setStrengthMax: actor data
// 75 STRENGTH and 76 STRENGTH_MAX, sent to Bedrock clients and saved with the
// mob as Strength/StrengthMax). minecraft:strength sets it when it is added;
// a bred baby gets it from a parent (BreedableComponent::_handleMate) without
// the component.

// Strength returns m's strength, 0 when it has none.
func Strength(m *mob.Mob) int {
	v, _ := m.ActorData(protocol.EntityDataKeyStrength)
	s, _ := v.(int32)
	return int(s)
}

// StrengthMax returns m's maximum strength, 0 when it has none.
func StrengthMax(m *mob.Mob) int {
	v, _ := m.ActorData(protocol.EntityDataKeyStrengthMax)
	s, _ := v.(int32)
	return int(s)
}

// HasStrength reports whether m has strength data.
func HasStrength(m *mob.Mob) bool {
	_, ok := m.ActorData(protocol.EntityDataKeyStrength)
	return ok
}

// SetStrength sets m's strength, within [0, StrengthMax] (Actor::setStrength).
func SetStrength(m *mob.Mob, v int) {
	m.SetActorData(protocol.EntityDataKeyStrength, int32(max(0, min(v, StrengthMax(m)))))
}

// SetStrengthMax sets m's maximum strength and lowers its strength to it
// (Actor::setStrengthMax).
func SetStrengthMax(m *mob.Mob, v int) {
	m.SetActorData(protocol.EntityDataKeyStrengthMax, int32(max(0, v)))
	if HasStrength(m) && Strength(m) > v {
		SetStrength(m, v)
	}
}
