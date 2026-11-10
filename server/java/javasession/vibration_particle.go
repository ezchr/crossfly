package javasession

import (
	"math"

	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

// Java's minecraft:position_source_type registry ids (PositionSourceType registers block, then entity).
const (
	positionSourceBlock  = 0
	positionSourceEntity = 1
)

// viewVibration shows a vibration signal flying from pos to its listener as Java's vibration particle
// (VibrationParticleOption: a PositionSource, then the arrival time in ticks): to a block listener's
// block (BlockPositionSource), or to an entity at its eye height (EntityPositionSource: entity id and y
// offset), as the warden's VibrationSystem does. Vanilla sends it with count 0 at the vibration's origin.
func (s *Session) viewVibration(pos mgl64.Vec3, v particle.Vibration) {
	id, ok := s.namedParticleID("vibration")
	if !ok || id < 0 {
		return
	}
	w := s.packet()
	w.VarInt(id)
	if v.Entity != nil {
		var eid int32
		if v.Entity == s.ent {
			eid = selfEntityID
		} else {
			s.entMu.Lock()
			eid, ok = s.entityIDs[v.Entity]
			s.entMu.Unlock()
			if !ok {
				s.writers.Put(w)
				return
			}
		}
		w.VarInt(positionSourceEntity)
		w.VarInt(eid)
		w.Float32(float32(v.EyeHeight))
	} else {
		w.VarInt(positionSourceBlock)
		w.Position(int(math.Floor(v.Pos[0])), int(math.Floor(v.Pos[1])), int(math.Floor(v.Pos[2])))
	}
	w.VarInt(int32(v.Ticks))
	s.particleAt(w, true, pos, 0, 0, 0, 0, 0)
}

// levelEventSculkShriek is Java's LevelEvent.PARTICLES_SCULK_SHRIEK, which SculkShriekerBlockEntity
// sends for a shriek at the shrieker's block.
const levelEventSculkShriek = 3007
