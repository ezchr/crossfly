//go:build crossfly_full

// Package boss holds the goals of the boss and special hostile mobs: the
// ender dragon's eight goals, the wither's two, the warden's sonic boom,
// roar, dig, emerge and investigation.
// Their state and the hard-coded actor logic are in
// server/mob/component/boss. See server/mob/component/boss/README.md.
package boss

import (
	"bytes"
	"encoding/json"

	"github.com/df-mc/dragonfly/server/mob"
)

func init() {
	mob.RegisterGoal("minecraft:behavior.dragonholdingpattern", newHoldingPattern)
	mob.RegisterGoal("minecraft:behavior.dragonstrafeplayer", newStrafePlayer)
	mob.RegisterGoal("minecraft:behavior.dragonchargeplayer", newChargePlayer)
	mob.RegisterGoal("minecraft:behavior.dragontakeoff", newTakeoff)
	mob.RegisterGoal("minecraft:behavior.dragonlanding", newLanding)
	mob.RegisterGoal("minecraft:behavior.dragonscanning", newScanning)
	mob.RegisterGoal("minecraft:behavior.dragonflaming", newFlaming)
	mob.RegisterGoal("minecraft:behavior.dragondeath", newDeath)

	mob.RegisterGoal("minecraft:behavior.wither_random_attack_pos_goal", newWitherAttackPos)
	mob.RegisterGoal("minecraft:behavior.wither_target_highest_damage", newWitherHighestDamage)

	mob.RegisterGoal("minecraft:behavior.sonic_boom", newSonicBoom)
	mob.RegisterGoal("minecraft:behavior.roar", newRoar)
	mob.RegisterGoal("minecraft:behavior.dig", newDig)
	mob.RegisterGoal("minecraft:behavior.emerge", newEmerge)
	mob.RegisterGoal("minecraft:behavior.investigate_suspicious_location", newInvestigate)
}

// jsonRaw keeps a field's raw JSON.
type jsonRaw json.RawMessage

func (r *jsonRaw) UnmarshalJSON(b []byte) error {
	*r = append((*r)[:0], b...)
	return nil
}

func (r jsonRaw) raw() json.RawMessage {
	if len(bytes.TrimSpace(r)) == 0 {
		return nil
	}
	return json.RawMessage(r)
}

func or[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}
