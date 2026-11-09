//go:build crossfly_full

package misc

import (
	"sync"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Block break dispatch for minecraft:block_sensor. BDS: BlockSource block
// changes call Dimension::onBlockChanged, which hands every change whose
// block type differs (old vs new BlockLegacy) to
// BlockEventDispatcher::dispatchEvent with the old block and the actor that
// caused it (nil when none). The dispatcher only visits listeners that were
// registered by BlockBreakSensorComponent::initialize, each with its own
// radius around the mob's AABB centre (dist² <= radius²). Here
// world.BlockBroken is that dispatch (players breaking blocks, explosions
// with the exploding entity as the actor) and blockSensors the listener
// list, so a break with no block sensor in the world costs one atomic load.
func init() {
	world.RegisterBlockBreakHook(BlockBroken)
}

// blockSensors holds the mobs with an attached BlockSensor per world.
// Mobs that are closed or unloaded without a Detach are dropped the next
// time a break in their world finds them gone.
var blockSensors sensorSet

type sensorSet struct {
	mu  sync.Mutex
	n   atomic.Int32
	byW map[*world.World]map[*world.EntityHandle]struct{}
}

func (s *sensorSet) add(m *mob.Mob) {
	tx := m.Tx()
	if tx == nil || m.H() == nil {
		return
	}
	w := tx.World()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byW == nil {
		s.byW = map[*world.World]map[*world.EntityHandle]struct{}{}
	}
	set := s.byW[w]
	if set == nil {
		set = map[*world.EntityHandle]struct{}{}
		s.byW[w] = set
	}
	if _, ok := set[m.H()]; !ok {
		set[m.H()] = struct{}{}
		s.n.Add(1)
	}
}

func (s *sensorSet) remove(m *mob.Mob) {
	if tx := m.Tx(); tx != nil {
		s.drop(tx.World(), m.H())
	}
}

func (s *sensorSet) drop(w *world.World, h *world.EntityHandle) {
	s.mu.Lock()
	defer s.mu.Unlock()
	set := s.byW[w]
	if _, ok := set[h]; !ok {
		return
	}
	delete(set, h)
	s.n.Add(-1)
	if len(set) == 0 {
		delete(s.byW, w)
	}
}

// handles returns a copy of the sensor handles of w: events fired while
// the caller walks them may attach or detach sensors.
func (s *sensorSet) handles(w *world.World) []*world.EntityHandle {
	s.mu.Lock()
	defer s.mu.Unlock()
	set := s.byW[w]
	if len(set) == 0 {
		return nil
	}
	hs := make([]*world.EntityHandle, 0, len(set))
	for h := range set {
		hs = append(hs, h)
	}
	return hs
}

func boxCentre(b cube.BBox) mgl64.Vec3 {
	return b.Min().Add(b.Max()).Mul(0.5)
}
