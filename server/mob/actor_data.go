//go:build crossfly_full

package mob

import "maps"

// actorDataKey keys the extra actor data in the mob's scratch data.
type actorDataKey struct{}

// SetActorData sets one extra Bedrock actor data entry (a gophertunnel
// protocol.EntityDataKey*) that no Meta field covers: the wither's head
// targets and invulnerable ticks, the warden's heartbeat, ... and shows it
// to viewers when it changed. Values are int8/int16/int32/int64/float32/
// string, or a *world.EntityHandle, which the Bedrock session sends as that
// entity's runtime id (-1 for nil). nil removes the entry. The Java session
// reads the same entries for the mobs it maps (javasession/mobdata_boss.go).
func (m *Mob) SetActorData(key uint32, v any) {
	d, _ := m.s.scratch[actorDataKey{}].(map[uint32]any)
	if v == nil {
		if _, ok := d[key]; !ok {
			return
		}
		delete(d, key)
	} else {
		if old, ok := d[key]; ok && old == v {
			return
		}
		if d == nil {
			d = map[uint32]any{}
			m.SetData(actorDataKey{}, d)
		}
		d[key] = v
	}
	m.RefreshState()
}

// ActorData returns an extra actor data entry set with SetActorData.
func (m *Mob) ActorData(key uint32) (any, bool) {
	d, _ := m.s.scratch[actorDataKey{}].(map[uint32]any)
	v, ok := d[key]
	return v, ok
}

// ExtraActorData returns a copy of the extra actor data (server/session and
// server/java/javasession probe it on the mob and its behaviour).
func (s *state) ExtraActorData() map[uint32]any {
	d, _ := s.scratch[actorDataKey{}].(map[uint32]any)
	if len(d) == 0 {
		return nil
	}
	return maps.Clone(d)
}

// ExtraActorData returns a copy of the extra actor data.
func (m *Mob) ExtraActorData() map[uint32]any { return m.s.ExtraActorData() }
