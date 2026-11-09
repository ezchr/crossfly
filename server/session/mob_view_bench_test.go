package session

import (
	"bytes"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// BenchmarkMobMovementView is what one mob update costs a Bedrock viewer: the
// MoveActorAbsolute and SetActorMotion a moving mob is shown with each tick,
// made, queued and encoded as on the wire. Multiply by the moves and motions
// per tick of BenchmarkMobViewers (server/mob/internal/mobbench).
func BenchmarkMobMovementView(b *testing.B) {
	s := newTestSession()
	s.packets = make(chan packet.Packet, 16)
	m := &fakeMob{h: world.EntitySpawnOpts{}.New(&fakeMobType{id: "test:bench"}, fakeMobConfig{}), head: 10, body: 5, health: 10, maxHealth: 10}
	s.ViewEntity(m)
	written(s)
	buf := new(bytes.Buffer)
	w := protocol.NewWriter(buf, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		f := float64(i % 100)
		s.ViewEntityMovement(m, mgl64.Vec3{f * 0.1, 64, 3}, cube.Rotation{f, 0}, true)
		s.ViewEntityVelocity(m, mgl64.Vec3{0.1, -0.0784, f * 0.001})
		for range 2 {
			pk := <-s.packets
			buf.Reset()
			pk.Marshal(w)
		}
	}
}
