//go:build crossfly_full

package boss

import (
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world/gameevent"
)

// Game events mobs emit through the emit_vibration event response
// (creaking, armadillo), with their VibrationListenerUtils::
// getGameEventFrequency frequencies (GameEventRegistry ids 24 and 17).
const (
	VibrationAct = "entity_act"
	VibrationDie = "entity_die"
)

func init() {
	vibrationFrequency[VibrationAct] = 4
	vibrationFrequency[VibrationDie] = 15
	mob.RegisterVibrationHook(emitVibration)
}

// emitVibration posts a game event a mob emitted (EmitVibrationResponse).
func emitVibration(m *mob.Mob, event string) {
	ev, ok := map[string]gameevent.Event{VibrationAct: gameevent.EntityAct, VibrationDie: gameevent.EntityDie}[event]
	if !ok {
		return
	}
	gameevent.PostContext(m.Tx(), ev, m.Position(), gameevent.Context{Source: m, Damped: m.HasComponent("minecraft:vibration_damper")})
}
