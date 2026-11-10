package session

import (
	"bytes"
	"math"

	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// viewVibration shows a vibration signal flying from pos to its listener: BDS
// VibrationListener::_requestVibrationParticle sends LevelEventGeneric 2027 with the origin and the
// target as GameEvents::PositionSource tags (type "vec3" with x, y, z, or "actor" with uniqueID and
// attachPos 3, the head), speed 20 and timeToLive in seconds.
func (s *Session) viewVibration(pos mgl64.Vec3, v particle.Vibration) {
	target := map[string]any{"type": "vec3", "x": float32(v.Pos[0]), "y": float32(v.Pos[1]), "z": float32(v.Pos[2])}
	if v.Entity != nil {
		target = map[string]any{"type": "actor", "uniqueID": int64(s.handleRuntimeID(v.Entity)), "attachPos": int32(3)}
	}
	buf := bytes.NewBuffer(nil)
	_ = nbt.NewEncoderWithEncoding(buf, nbt.NetworkLittleEndian).Encode(map[string]any{
		"origin":     map[string]any{"type": "vec3", "x": float32(pos[0]), "y": float32(pos[1]), "z": float32(pos[2])},
		"target":     target,
		"speed":      float32(20),
		"timeToLive": float32(v.Ticks) * 0.05,
	})
	s.levelEventGeneric(packet.LevelEventParticlesVibrationSignal, buf.Bytes())
}

// viewShriek shows a sculk shrieker's shriek at the block at pos: SculkShriekerBlockActorInternal::_shriek
// broadcasts level event 2035 with originX, originY and originZ.
func (s *Session) viewShriek(pos mgl64.Vec3) {
	buf := bytes.NewBuffer(nil)
	_ = nbt.NewEncoderWithEncoding(buf, nbt.NetworkLittleEndian).Encode(map[string]any{
		"originX": int32(math.Floor(pos[0])),
		"originY": int32(math.Floor(pos[1])),
		"originZ": int32(math.Floor(pos[2])),
	})
	s.levelEventGeneric(packet.LevelEventParticleSculkShriek, buf.Bytes())
}

// levelEventGeneric sends a LevelEventGeneric with the fields of an encoded compound: the packet carries
// them without the compound's name and end tag, as in ViewSleepingPlayers.
func (s *Session) levelEventGeneric(id int32, data []byte) {
	s.writePacket(&packet.LevelEventGeneric{
		EventID:             id,
		SerialisedEventData: data[2 : len(data)-1],
	})
}
