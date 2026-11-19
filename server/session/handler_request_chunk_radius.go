package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// ChunkRadiusForDevice, when set, caps the view distance of a Bedrock player by the device they
// play on, so that weak devices can be sent fewer chunks than strong ones. It only ever lowers the
// server's maximum: ok false, or a radius above it, leaves the maximum. Set it before the server
// starts; it applies to every view-distance request.
var ChunkRadiusForDevice func(protocol.DeviceOS) (radius int, ok bool)

// maxRadius is the most chunks this session is sent around its player: the server's maximum, or
// less for its device (ChunkRadiusForDevice).
func (s *Session) maxRadius() int32 {
	m := s.maxChunkRadius
	if ChunkRadiusForDevice != nil && s.conn != nil {
		if r, ok := ChunkRadiusForDevice(s.conn.ClientData().DeviceOS); ok && r > 0 && int32(r) < m {
			m = int32(r)
		}
	}
	return m
}

// RequestChunkRadiusHandler handles the RequestChunkRadius packet.
type RequestChunkRadiusHandler struct {
	// seen: the session sent a request before. exempt: one of this machine's own services, not
	// checked for bedrocktool (toolcheck.go).
	seen, exempt bool
}

// Handle ...
func (h *RequestChunkRadiusHandler) Handle(p packet.Packet, s *Session, tx *world.Tx, _ Controllable) error {
	pk := p.(*packet.RequestChunkRadius)
	if h.toolCheck(pk, s) {
		return nil
	}

	if m := s.maxRadius(); pk.ChunkRadius > m {
		pk.ChunkRadius = m
	}
	s.chunkRadius = pk.ChunkRadius

	s.chunkLoader.ChangeRadius(tx, int(pk.ChunkRadius))

	s.writePacket(&packet.ChunkRadiusUpdated{ChunkRadius: s.chunkRadius})
	return nil
}
