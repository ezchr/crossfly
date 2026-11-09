//go:build crossfly_full

package mob

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// Owners (BDS ActorOwnerComponent, saved as OwnerNew). A tamed mob keeps its
// owner's UUID, saved with the mob as mob:owner, and finds the owner by it
// when the player is back in the world: Dragonfly gives a player a new handle
// every time it joins. Meta.Owner is the handle shown to clients.

type ownerKey struct{}

// SetOwner makes e the mob's owner (nil forgets the owner) and shows it.
func (m *Mob) SetOwner(e world.Entity) {
	if e == nil {
		m.SetData(ownerKey{}, nil)
		m.UpdateMeta(func(meta *Meta) { meta.Owner = nil })
		return
	}
	m.SetData(ownerKey{}, e.H().UUID())
	m.UpdateMeta(func(meta *Meta) { meta.Owner = e.H() })
}

// OwnerUUID returns the UUID of the mob's owner, if it has one.
func (m *Mob) OwnerUUID() (uuid.UUID, bool) {
	if id, ok := m.s.scratch[ownerKey{}].(uuid.UUID); ok {
		return id, true
	}
	if h := m.s.meta.Owner; h != nil {
		return h.UUID(), true
	}
	return uuid.Nil, false
}

// OwnerEntity returns the mob's owner if it is in the mob's world, looking a
// player up by the saved UUID when its handle is not known (yet).
func (m *Mob) OwnerEntity() (world.Entity, bool) {
	tx := m.Tx()
	if tx == nil {
		return nil, false
	}
	if h := m.s.meta.Owner; h != nil {
		if e, ok := h.Entity(tx); ok {
			return e, true
		}
	}
	id, ok := m.s.scratch[ownerKey{}].(uuid.UUID)
	if !ok {
		return nil, false
	}
	for p := range tx.Players() {
		if p.H().UUID() == id {
			if m.s.meta.Owner != p.H() {
				m.UpdateMeta(func(meta *Meta) { meta.Owner = p.H() })
			}
			return p, true
		}
	}
	return nil, false
}

// encodeOwner and decodeOwner save the owner UUID (nbt.go).
func (s *state) encodeOwner(out map[string]any) {
	if id, ok := s.scratch[ownerKey{}].(uuid.UUID); ok {
		out["mob:owner"] = id.String()
	} else if h := s.meta.Owner; h != nil {
		out["mob:owner"] = h.UUID().String()
	}
}

func (s *state) decodeOwner(d map[string]any) {
	str, ok := d["mob:owner"].(string)
	if !ok {
		return
	}
	if id, err := uuid.Parse(str); err == nil {
		if s.scratch == nil {
			s.scratch = map[any]any{}
		}
		s.scratch[ownerKey{}] = id
	}
}
