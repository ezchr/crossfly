//go:build crossfly_full

package boss

import "github.com/df-mc/dragonfly/server/mob"

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

// emitVibration offers a game event a mob emitted to the vibration
// listeners within 16 blocks of it.
func emitVibration(m *mob.Mob, event string) {
	tx := m.Tx()
	for e := range tx.EntitiesWithin(m.BBox().Grow(16)) {
		lm, ok := mob.Of(e)
		if !ok || lm.H() == m.H() {
			continue
		}
		if l, ok := ListenerOf(lm); ok {
			l.Hear(lm, Vibration{Event: event, Pos: m.Position(), Source: m.H()})
		}
	}
}
