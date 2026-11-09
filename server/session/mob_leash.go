package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// Leads (server/mob/component/core): a leashed mob carries its holder in the actor data LEASH_HOLDER, sent
// as the holder's runtime ID. A holder this client has not been shown yet (a fence knot or a mob later in
// the chunk) has no runtime ID, and one shown again gets a new one, so the holder of every leashed mob shown
// is remembered and its runtime ID sent again whenever it comes into view.

// noteLeashHolder remembers the leash holder of the entity e (v is its LEASH_HOLDER actor data: a
// *world.EntityHandle, or -1 once it is unleashed).
func (s *Session) noteLeashHolder(e any, v any) {
	ent, ok := e.(world.Entity)
	if !ok {
		return
	}
	h, _ := v.(*world.EntityHandle)
	s.entityMutex.Lock()
	defer s.entityMutex.Unlock()
	id, ok := s.entityRuntimeIDs[ent.H()]
	if !ok {
		return
	}
	if h == nil {
		delete(s.leashHolders, id)
		return
	}
	if s.leashHolders == nil {
		s.leashHolders = map[uint64]*world.EntityHandle{}
	}
	s.leashHolders[id] = h
}

// viewLeashHolder sends the LEASH_HOLDER of the mobs leashed to e, which came into view.
func (s *Session) viewLeashHolder(e world.Entity) {
	s.entityMutex.RLock()
	if len(s.leashHolders) == 0 {
		s.entityMutex.RUnlock()
		return
	}
	holder, ok := s.entityRuntimeIDs[e.H()]
	var mobs []uint64
	if ok {
		for id, h := range s.leashHolders {
			if h == e.H() {
				mobs = append(mobs, id)
			}
		}
	}
	s.entityMutex.RUnlock()
	for _, id := range mobs {
		s.writePacket(&packet.SetActorData{
			EntityRuntimeID: id,
			EntityMetadata:  protocol.EntityMetadata{protocol.EntityDataKeyLeashHolder: int64(holder)},
		})
	}
}
