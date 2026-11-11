package player

import (
	"context"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// countViewer counts the movement and velocity a viewer is shown of other entities.
type countViewer struct {
	world.NopViewer
	moves, vels int
}

func (v *countViewer) ViewEntityMovement(world.Entity, mgl64.Vec3, cube.Rotation, bool) { v.moves++ }
func (v *countViewer) ViewEntityVelocity(world.Entity, mgl64.Vec3)                      { v.vels++ }

// TestServerPlayerFallSendsNoVelocity: a player without a connection falling for 20 ticks is shown at
// most one move a tick and no velocity: gravity changing its velocity is not news to viewers.
func TestServerPlayerFallSendsNoVelocity(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	v := &countViewer{}
	_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		l := world.NewLoader(2, w, v)
		l.Move(tx, mgl64.Vec3{0.5, 100, 0.5})
		l.Load(tx, 25)
		pos := mgl64.Vec3{0.5, 100, 0.5}
		h := world.EntitySpawnOpts{Position: pos}.New(Type, Config{
			Name: "Bot", UUID: uuid.New(), GameMode: world.GameModeSurvival, Health: 20, MaxHealth: 20, Position: pos,
		})
		p := tx.AddEntity(h).(*Player)
		v.moves, v.vels = 0, 0
		for i := int64(0); i < 20; i++ {
			p.Tick(tx, i)
		}
		if p.Position()[1] >= 100 {
			t.Fatalf("player did not fall: %v", p.Position())
		}
		if v.vels != 0 || v.moves > 20 {
			t.Fatalf("20 ticks of falling: %d moves, %d velocities shown; want <= 20 and 0", v.moves, v.vels)
		}
		// A velocity the server sets is shown (knockback).
		p.KnockBack(p.Position().Add(mgl64.Vec3{1, 0, 0}), 0.4, 0.4)
		return struct{}{}, nil
	})
}
