//go:build crossfly_full

package mob

// vibrationHooks are told about the game events mobs emit (package
// mob/component/boss passes them to the vibration listeners near the mob).
var vibrationHooks []func(m *Mob, event string)

// RegisterVibrationHook registers f to be called with every game event a mob
// emits (EmitVibration). Call it from an init function.
func RegisterVibrationHook(f func(m *Mob, event string)) {
	vibrationHooks = append(vibrationHooks, f)
}

// EmitVibration emits a game event ("entity_act", "entity_die", ...) at the
// mob, as GameEventUtils::requestDeferredPosting does for the emit_vibration
// event response.
func (m *Mob) EmitVibration(event string) {
	if m.Tx() == nil || m.closed() {
		return
	}
	for _, h := range vibrationHooks {
		h(m, event)
	}
}
