//go:build crossfly_full

package core

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// Ambient sounds as BDS plays them: the server never sends one. Every mob
// has actor data 108 (interval, seconds), 109 (random extra range) and 110
// (the level sound event), and the Bedrock client plays the ambient sound
// from them on its own timer: interval + random(range) seconds.
//
//   - defineEntityData: 8 s, 16 s, "ambient";
//   - the reloadHardcoded of a class whose definition has no
//     minecraft:ambient_sound_interval: Animal 12 s, Horse 400 s, Squid 6 s,
//     Ghast 5 s (Guardian sets the default 8 s again);
//   - minecraft:ambient_sound_interval (AmbientSoundIntervalDefinition::
//     initialize) sets all three, and AmbientSoundServerSystem updates 110 from
//     event_names every round(value*10) ticks.
//
// The Java client plays nothing by itself, so the server plays the sound at
// the same rate for Java viewers only (mob.Sound.ClientSide; the Bedrock
// session drops it).
const ambientDefaultRange = 16 // defineEntityData, actor data 109

// ambientKey is the component key whose presence turns the hardcoded
// defaults off.
const ambientKey = "minecraft:ambient_sound_interval"

func init() {
	mob.RegisterInitHook(mob.InitHook{Name: "core/ambient_data", Init: setDefaultAmbientData})
	mob.RegisterSystem(mob.System{Name: "core/hardcoded_ambient", Stage: mob.StageSensors, Order: 30, Tick: hardcodedAmbientTick})
}

// hardcodedAmbientInterval is the ambient interval (actor data 108) of an
// entity type without minecraft:ambient_sound_interval.
func hardcodedAmbientInterval(id string) float64 {
	switch id {
	// Animal::reloadHardcoded: the Animal classes (BDS 1.21.3 actor
	// factory) that have no ambient_sound_interval in their files.
	case "minecraft:cow", "minecraft:sheep", "minecraft:mooshroom", "minecraft:goat", "minecraft:rabbit",
		"minecraft:parrot", "minecraft:camel", "minecraft:camel_husk", "minecraft:llama", "minecraft:trader_llama",
		"minecraft:ocelot", "minecraft:sniffer", "minecraft:strider", "minecraft:turtle":
		return 12
	case "minecraft:horse", "minecraft:donkey", "minecraft:mule", "minecraft:skeleton_horse", "minecraft:zombie_horse":
		return 400 // Horse::reloadHardcoded
	case "minecraft:squid", "minecraft:glow_squid":
		return 6 // Squid::reloadHardcoded
	case "minecraft:ghast":
		return 5 // Ghast::reloadHardcoded
	}
	return 8 // defineEntityData (Guardian::reloadHardcoded: 8 too)
}

// setAmbientData sets actor data 108-110.
func setAmbientData(m *mob.Mob, interval, rng float64, event string) {
	m.SetActorData(protocol.EntityDataKeyAmbientSoundInterval, float32(interval))
	m.SetActorData(protocol.EntityDataKeyAmbientSoundIntervalRange, float32(rng))
	m.SetActorData(protocol.EntityDataKeyAmbientSoundEventName, event)
}

// setDefaultAmbientData sets the data of a mob without
// minecraft:ambient_sound_interval (also when that component goes).
func setDefaultAmbientData(m *mob.Mob) {
	setAmbientData(m, hardcodedAmbientInterval(m.Identifier()), ambientDefaultRange, "ambient")
}

type hardcodedAmbientKey struct{}

// hardcodedAmbient is the per-mob countdown in ticks.
type hardcodedAmbient struct{ left int }

// hardcodedAmbientTick plays the default ambient sound of a mob without
// minecraft:ambient_sound_interval for Java viewers, at the rate the Bedrock
// client plays it.
func hardcodedAmbientTick(m *mob.Mob) {
	if m.HasComponent(ambientKey) {
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
	case "ambient.in.air":
		ev = "ambient" // Java has one ambient sound for flying and walking (ghast, bat)
	default:
		return // ambient.in.water: no Java sound for these mobs
	}
	playClientAmbient(m, ev)
}

func (st *hardcodedAmbient) reset(m *mob.Mob) {
	// A shared generator, not m.Rand(): this purely cosmetic timer of every
	// mob must not shift the rolls of its AI.
	st.left = ticksOf(hardcodedAmbientInterval(m.Identifier()) + rand.Float64()*ambientDefaultRange)
}

// playClientAmbient plays an ambient sound that the Bedrock client plays by
// itself: only Java viewers get it.
func playClientAmbient(m *mob.Mob, event string) {
	if event == "" {
		return
	}
	event = ClassAmbientEvent(m, event) // class_ambient.go
	m.Tx().PlaySound(m.Position(), MobSound{Event: event, Identifier: m.Identifier(), Baby: IsBaby(m), ClientSide: true})
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
