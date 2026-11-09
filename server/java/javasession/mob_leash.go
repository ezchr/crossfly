package javasession

import (
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// Leads (server/mob/component/core): Java shows a leashed mob's lead from set_entity_link (Leashable,
// ClientboundSetEntityLinkPacket: the mob's id and the holder's, 0 for none). The holder of a mob comes from
// its Bedrock LEASH_HOLDER actor data (Mob.ActorData). A link to a holder this client does not have yet is
// sent when the holder comes into view, and again when it comes back.

// leashLink is the lead of one mob this client sees.
type leashLink struct {
	holder *world.EntityHandle
	// sent is the holder id the last link named (0: none sent yet).
	sent int32
}

// mobLeashHolder is implemented by mobs (server/mob.Mob.ActorData).
type mobLeashHolder interface {
	ActorData(key uint32) (any, bool)
}

// viewLeash sends the lead of e (Java id id) when it changed.
func (s *Session) viewLeash(e world.Entity, id int32) {
	ad, ok := e.(mobLeashHolder)
	if !ok {
		return
	}
	v, _ := ad.ActorData(protocol.EntityDataKeyLeashHolder)
	h, _ := v.(*world.EntityHandle)
	s.entMu.Lock()
	l := s.leashes[id]
	if h == nil {
		if l == nil {
			s.entMu.Unlock()
			return
		}
		delete(s.leashes, id)
		s.entMu.Unlock()
		if l.sent != 0 {
			s.sendLeashLink(id, 0)
		}
		return
	}
	if l == nil {
		if s.leashes == nil {
			s.leashes = map[int32]*leashLink{}
		}
		l = &leashLink{}
		s.leashes[id] = l
	}
	l.holder = h
	hid, known := s.leashHolderID(h)
	send := known && l.sent != hid
	if send {
		l.sent = hid
	}
	s.entMu.Unlock()
	if send {
		s.sendLeashLink(id, hid)
	}
}

// leashHolderID is the Java id of a holder, if this client has it. entMu must be held.
func (s *Session) leashHolderID(h *world.EntityHandle) (int32, bool) {
	if h == s.ent {
		return selfEntityID, true
	}
	id, ok := s.entityIDs[h]
	return id, ok
}

// viewLeashHolder sends the leads of the mobs leashed to e (Java id id), which came into view.
func (s *Session) viewLeashHolder(e world.Entity, id int32) {
	s.entMu.Lock()
	var mobs []int32
	for mid, l := range s.leashes {
		if l.holder == e.H() && l.sent != id {
			l.sent = id
			mobs = append(mobs, mid)
		}
	}
	s.entMu.Unlock()
	for _, mid := range mobs {
		s.sendLeashLink(mid, id)
	}
}

// forgetLeash forgets the lead of a mob that left view (Java id id); the leads to h, if it was a holder,
// are sent again when it comes back.
func (s *Session) forgetLeash(h *world.EntityHandle, id int32) {
	s.entMu.Lock()
	delete(s.leashes, id)
	for _, l := range s.leashes {
		if l.holder == h {
			l.sent = 0
		}
	}
	s.entMu.Unlock()
}

// sendLeashLink sends set_entity_link: mob is tied to holder (0: to nothing).
func (s *Session) sendLeashLink(mob, holder int32) {
	w := s.packet()
	w.Int32(mob)
	w.Int32(holder)
	s.queue(v777.ClientboundPlaySetEntityLink, w)
}
