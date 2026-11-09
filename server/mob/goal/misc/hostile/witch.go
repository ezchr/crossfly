//go:build crossfly_full

package hostile

import (
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// The witch's hardcoded parts (BDS class Witch): the witch JSON has no
// component for them, so they are gated by identifier.
const witchID = "minecraft:witch"

func init() {
	mob.RegisterSystem(mob.System{Name: "misc-hostile/witch", Stage: mob.StageSensors, Order: 210, Tick: witchPreAiStep})
	mob.RegisterDamageHook(mob.DamageHook{Name: "misc-hostile/witch", Modify: WitchDamage})
}

// witchPreAiStep is Witch::preAiStep: the witch avoids water only while it
// has no target, and with chance 0.00075 a tick it shows the hat magic
// (ActorEvent 24, Witch::handleEntityEvent: 10-44 client-side particles;
// Java entity event 15).
func witchPreAiStep(m *mob.Mob) {
	if m.Identifier() != witchID {
		return
	}
	if n, ok := m.Navigation().(interface{ SetAvoidWater(bool) }); ok {
		_, hasTarget := m.Target()
		n.SetAvoidWater(!hasTarget)
	}
	if m.Rand().Float64() < 0.00075 {
		kit.Show(m, kit.ActorEvent(packet.ActorEventWitchHatMagic))
	}
}

// WitchDamage is Witch::getDamageAfterEnchantReduction for a hit on the
// witch m: damage the witch caused itself is dropped (unless it is
// self_destruct, i.e. /kill), and magic damage (instant damage, poison) is
// cut to 15%. Other damage is left as it is.
func WitchDamage(m *mob.Mob, d *mob.Damage) {
	if m.Identifier() != witchID {
		return
	}
	cause := core.CauseOf(d.Source)
	if d.Attacker != nil && d.Attacker.H() == m.H() && cause != "self_destruct" {
		d.Cancel = true
		return
	}
	if cause == "magic" {
		d.Amount *= 0.15
	}
}
