//go:build crossfly_full

package animal

import (
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// The pufferfish's hardcoded parts (BDS class Pufferfish), gated by
// identifier. Its puffing up near players and other mobs and deflating
// again is all in pufferfish.json (entity sensors, timers and the variant
// of each puff group).
const pufferfishID = "minecraft:pufferfish"

func init() {
	mob.RegisterHurtHook(mob.HurtHook{Name: "animal/pufferfish_hurt", Hurt: pufferfishHurt})
	mob.RegisterSystem(mob.System{Name: "animal/pufferfish_puff_state", Stage: mob.StageEnvironment, Order: 300, Tick: pufferfishPuffState})
}

// pufferfishHurt is Pufferfish::_hurt: a pufferfish that is still alive
// after a hit puffs up at once (minecraft:to_full_puff).
func pufferfishHurt(m *mob.Mob, _ float64, _ world.DamageSource) {
	if m.Identifier() != pufferfishID || m.Health() <= 0 {
		return
	}
	m.Fire("minecraft:to_full_puff", nil)
	pufferfishPuffState(m)
}

// pufferfishPuffState is Pufferfish::normalTick: actor data 86 (PUFFED_STATE,
// a byte) follows the variant of the puff group, 0 normal, 1 half and 2 full
// puff. The Bedrock client draws the puffed model from it, not from the
// variant; the Java session sends the variant as Pufferfish.PUFF_STATE.
func pufferfishPuffState(m *mob.Mob) {
	if m.Identifier() != pufferfishID {
		return
	}
	m.SetActorData(protocol.EntityDataKeyPuffedState, int8(m.Meta().Variant))
}
