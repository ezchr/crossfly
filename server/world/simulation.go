package world

import "github.com/go-gl/mathgl/mgl64"

// simulation is the chunks of the viewers of a World, worked out once per
// transaction for Tx.Simulated.
type simulation struct {
	tx      *Tx
	r       int32
	loaders []ChunkPos
}

// Simulated reports whether pos is within the simulation distance of a
// viewer: whether its chunk is within the World's tick range (in chunks,
// SetTickRange) of the chunk a viewer is in. Bedrock Edition ticks entities
// only there, while further chunks may still be loaded and shown. A World
// without viewers reports true: it only ticks at all when it is
// synchronous, and then ticks everything.
func (tx *Tx) Simulated(pos mgl64.Vec3) bool {
	w := tx.World()
	s := &w.sim
	if s.tx != tx {
		s.tx, s.r = tx, int32(w.tickRange())
		s.loaders = s.loaders[:0]
		w.viewerMu.Lock()
		for l := range w.viewers {
			l.mu.RLock()
			s.loaders = append(s.loaders, l.pos)
			l.mu.RUnlock()
		}
		w.viewerMu.Unlock()
	}
	if len(s.loaders) == 0 {
		return true
	}
	c := chunkPosFromVec3(pos)
	for _, l := range s.loaders {
		dx, dz := l[0]-c[0], l[1]-c[1]
		if dx*dx+dz*dz <= s.r*s.r {
			return true
		}
	}
	return false
}
