package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// webEntity is an entity with a velocity that is, or is not, moved by a client.
type webEntity struct {
	world.Entity
	client bool
	vel    mgl64.Vec3
	sets   int
}

func (e *webEntity) Velocity() mgl64.Vec3     { return e.vel }
func (e *webEntity) SetVelocity(v mgl64.Vec3) { e.vel, e.sets = v, e.sets+1 }
func (e *webEntity) MovesClientSide() bool    { return e.client }

// TestCobwebLeavesClientMovedPlayers: a cobweb slows a server-moved entity itself, and sends nothing
// to an entity a client moves, whose game slows it (a velocity every tick made it fall fast).
func TestCobwebLeavesClientMovedPlayers(t *testing.T) {
	server := &webEntity{vel: mgl64.Vec3{1, -1, 1}}
	Cobweb{}.EntityInside(cube.Pos{}, nil, server)
	if server.sets != 1 || server.vel != (mgl64.Vec3{0.25, -0.05, 0.25}) {
		t.Fatalf("server-moved entity: %d velocity sets, velocity %v", server.sets, server.vel)
	}
	client := &webEntity{client: true, vel: mgl64.Vec3{1, -1, 1}}
	Cobweb{}.EntityInside(cube.Pos{}, nil, client)
	if client.sets != 0 {
		t.Fatalf("client-moved entity was sent a velocity %v", client.vel)
	}
}
