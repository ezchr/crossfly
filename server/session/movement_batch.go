package session

import (
	"sync"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// moveBatch holds the entity moves of a session not sent yet. Moves of other entities are kept until
// the session's player ticks (FlushEntityMovement), so that a viewer gets at most one MoveActorAbsolute
// per entity per tick, with the latest position, however often the entity moved in between (Java
// clients send several moves a tick, server-moved players were sent from more than one path).
type moveBatch struct {
	mu   sync.Mutex
	tick int64
	// pending is the latest unsent move by runtime ID.
	pending map[uint64]*packet.MoveActorAbsolute
	// sent is the tick of the last move sent by runtime ID. An entity without one gets its first
	// move at once.
	sent map[uint64]int64
}

// Distance tiers of entity moves sent to Bedrock viewers: within MoveNearDistance (8) blocks of the
// viewer an entity moves every tick (combat and anything in reach stays exact), up to
// MoveMidDistance (16) every 2 ticks, up to MoveFarDistance (32) every 3, beyond that every 4.
// Set them before the server starts. Teleports, server-made moves and the first move after an
// entity is shown are never held back, and the last position of an entity that stopped is sent
// on its next due tick.
var (
	MoveNearDistance   = 8.0
	MoveMidDistance    = 16.0
	MoveFarDistance    = 32.0
	MoveMidInterval    = int64(2)
	MoveFarInterval    = int64(3)
	MoveBeyondInterval = int64(4)
)

// MoveTiers are the distances of the tiers for one viewer: moves every tick up to Near, every
// MoveMidInterval ticks up to Mid, every MoveFarInterval up to Far, every MoveBeyondInterval beyond.
type MoveTiers struct{ Near, Mid, Far float64 }

// MoveTiersForDevice gives the tiers of a viewer by the device they play on, so that weak devices
// can get fewer moves than strong ones. nil, or ok false: MoveNearDistance, MoveMidDistance and
// MoveFarDistance. Set it before the server starts.
var MoveTiersForDevice func(protocol.DeviceOS) (tiers MoveTiers, ok bool)

// moveTiers returns the tiers of this session.
func (s *Session) moveTiers() MoveTiers {
	if MoveTiersForDevice != nil && s.conn != nil {
		if t, ok := MoveTiersForDevice(s.conn.ClientData().DeviceOS); ok {
			return t
		}
	}
	return MoveTiers{Near: MoveNearDistance, Mid: MoveMidDistance, Far: MoveFarDistance}
}

func (t MoveTiers) interval(dist float64) int64 {
	switch {
	case dist <= t.Near:
		return 1
	case dist <= t.Mid:
		return max(MoveMidInterval, 1)
	case dist <= t.Far:
		return max(MoveFarInterval, 1)
	default:
		return max(MoveBeyondInterval, 1)
	}
}

// queueMove keeps pk as the latest move of its entity, or writes it at once if it is the first move
// of the entity since it was shown.
func (s *Session) queueMove(pk *packet.MoveActorAbsolute) {
	b := &s.moves
	b.mu.Lock()
	if b.pending == nil {
		b.pending, b.sent = map[uint64]*packet.MoveActorAbsolute{}, map[uint64]int64{}
	}
	if _, ok := b.sent[pk.EntityRuntimeID]; !ok {
		b.sent[pk.EntityRuntimeID] = b.tick
		delete(b.pending, pk.EntityRuntimeID)
		b.mu.Unlock()
		s.writePacket(pk)
		return
	}
	b.pending[pk.EntityRuntimeID] = pk
	b.mu.Unlock()
}

// dropMove forgets the unsent move of an entity: it was teleported (the teleport is newer) or
// removed. If forget is true, its next move is sent at once, as the first one.
func (s *Session) dropMove(id uint64, forget bool) {
	b := &s.moves
	b.mu.Lock()
	delete(b.pending, id)
	if forget {
		delete(b.sent, id)
	} else if b.sent != nil {
		b.sent[id] = b.tick
	}
	b.mu.Unlock()
}

// FlushEntityMovement sends the moves kept since the last tick that are due, the viewer being at
// viewer. Moves not due yet stay until they are, so the last position of an entity that stopped is
// always sent.
func (s *Session) FlushEntityMovement(viewer mgl64.Vec3) {
	if s == Nop {
		return
	}
	tiers := s.moveTiers()
	b := &s.moves
	b.mu.Lock()
	b.tick++
	var out []*packet.MoveActorAbsolute
	for id, pk := range b.pending {
		p := pk.Position
		dist := mgl64.Vec3{float64(p[0]), float64(p[1]), float64(p[2])}.Sub(viewer).Len()
		if b.tick-b.sent[id] < tiers.interval(dist) {
			continue
		}
		out = append(out, pk)
		b.sent[id] = b.tick
		delete(b.pending, id)
	}
	b.mu.Unlock()
	for _, pk := range out {
		s.writePacket(pk)
	}
}
