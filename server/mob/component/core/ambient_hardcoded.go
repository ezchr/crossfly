//go:build crossfly_full

package core

import (
	"github.com/df-mc/dragonfly/server/mob"
)

// Ambient sounds of the animals whose JSON has no
// minecraft:ambient_sound_interval (cow, sheep, mooshroom, goat, rabbit,
// parrot). BDS never sends these: Animal::reloadHardcoded only sets actor
// data 108 (interval) to 12 s when the definition has none, 109 (range)
// keeps the default 16 s and 110 (event) "ambient" (defineEntityData), and
// the Bedrock client plays the sound from that data on its own
// (AmbientSoundServerSystem only syncs the event name; no server code calls
// Actor::playAmbientSound). So the sound goes to Java only
// (mob.Sound.ClientSide), at the client's rate: every 12 + random(16)
// seconds, the event picked as Actor::getAmbientSound does.
const (
	hardcodedAmbientInterval = 12 // Animal::reloadHardcoded, actor data 108
	hardcodedAmbientRange    = 16 // defineEntityData, actor data 109
)

func init() {
	mob.RegisterSystem(mob.System{Name: "core/hardcoded_ambient", Stage: mob.StageSensors, Order: 30, Tick: hardcodedAmbientTick})
}

// hardcodedAmbientType reports whether the Bedrock client plays the
// Animal default ambient sound of the entity type.
func hardcodedAmbientType(id string) bool {
	switch id {
	case "minecraft:cow", "minecraft:sheep", "minecraft:mooshroom", "minecraft:goat", "minecraft:rabbit", "minecraft:parrot":
		return true
	}
	return false
}

type hardcodedAmbientKey struct{}

// hardcodedAmbient is the per-mob countdown in ticks.
type hardcodedAmbient struct{ left int }

func hardcodedAmbientTick(m *mob.Mob) {
	if !hardcodedAmbientType(m.Identifier()) || m.HasComponent("minecraft:ambient_sound_interval") {
		return
	}
	var st *hardcodedAmbient
	if v, ok := m.Data(hardcodedAmbientKey{}); ok {
		st = v.(*hardcodedAmbient)
	} else {
		st = &hardcodedAmbient{}
		st.reset(m)
		m.SetData(hardcodedAmbientKey{}, st)
	}
	if st.left--; st.left > 0 {
		return
	}
	st.reset(m)
	ev := HardcodedAmbientEvent(m)
	switch ev {
	case "ambient", "ambient.baby", "ambient.screamer":
	default:
		// ambient.in.water / ambient.in.air: no Java sound for these mobs.
		return
	}
	m.Tx().PlaySound(m.Position(), MobSound{Event: ev, Identifier: m.Identifier(), Baby: IsBaby(m), ClientSide: true})
}

func (st *hardcodedAmbient) reset(m *mob.Mob) {
	st.left = ticksOf(hardcodedAmbientInterval + m.Rand().Float64()*hardcodedAmbientRange)
}

// HardcodedAmbientEvent is the level sound event the Bedrock client plays
// as the ambient sound of m: Goat::getAmbientSound (screaming goats,
// variant 1: ambient.screamer), else Actor::getAmbientSound: head in water
// ambient.in.water, off the ground ambient.in.air, babies ambient.baby,
// else ambient. Parrot::getAmbientSound's 1-in-1000 imitation and
// Parrot::playAmbientSound's imitating of nearby mobs are not modelled.
func HardcodedAmbientEvent(m *mob.Mob) string {
	if m.Identifier() == "minecraft:goat" && m.Meta().Variant == 1 {
		return "ambient.screamer"
	}
	switch {
	case m.Physics().Underwater:
		return "ambient.in.water"
	case !m.OnGround():
		return "ambient.in.air"
	case IsBaby(m):
		return "ambient.baby"
	}
	return "ambient"
}
