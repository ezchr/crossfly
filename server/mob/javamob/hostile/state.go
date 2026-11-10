//go:build crossfly_full

package hostile

import "github.com/df-mc/dragonfly/server/mob"

// stateKey is the mob.Data key of a mob's per-type Java state.
type stateKey struct{}

// stateOf returns the per-type state of m, made by newState the first time.
// The state lives in the mob's data (it survives between ticks; the *mob.Mob
// does not).
func stateOf[T any](m *mob.Mob, newState func() *T) *T {
	if v, ok := m.Data(stateKey{}); ok {
		if s, ok := v.(*T); ok {
			return s
		}
	}
	s := newState()
	m.SetData(stateKey{}, s)
	return s
}

// existingState returns the per-type state of m if it has one of type T.
func existingState[T any](m *mob.Mob) (*T, bool) {
	v, ok := m.Data(stateKey{})
	if !ok {
		return nil, false
	}
	s, ok := v.(*T)
	return s, ok
}
