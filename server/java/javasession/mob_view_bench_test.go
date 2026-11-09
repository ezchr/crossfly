package javasession

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// BenchmarkMobMovementView is what one mob update costs a Java viewer: the
// move packet (a delta move, or a full position sync every 60 moves) and the
// set_entity_motion a moving mob is shown with each tick, encoded as on the
// wire. Multiply by the moves and motions per tick of BenchmarkMobViewers
// (server/mob/internal/mobbench).
func BenchmarkMobMovementView(b *testing.B) {
	s := fxTestSession()
	s.entityIDs = map[*world.EntityHandle]int32{}
	s.tracks = map[int32]*track{}
	m := newFakeMob("minecraft:cow")
	s.addEntityID(m)
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		f := float64(i % 100)
		s.ViewEntityMovement(m, mgl64.Vec3{f * 0.1, 64, 3}, cube.Rotation{f, 0}, true)
		s.ViewEntityVelocity(m, mgl64.Vec3{0.1, -0.0784, f * 0.001})
		s.recycle()
	}
}
