//go:build crossfly_full

package jnether

import (
	"context"
	"io"
	"log/slog"
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// testWorld is a synchronous world for the tests of this package.
type testWorld struct {
	t *testing.T
	w *world.World
}

func newTestWorld(t *testing.T, dim world.Dimension, diff world.Difficulty) *testWorld {
	t.Helper()
	types := append(entity.DefaultRegistry.Types(), player.Type)
	types = append(types, ProjectileTypes()...)
	conf := world.Config{
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		Entities:     entity.DefaultRegistry.Config().New(types),
		Synchronous:  true,
		RandSource:   rand.NewPCG(1, 2),
		SaveInterval: -1,
		Dim:          dim,
	}
	w := conf.New()
	w.SetDifficulty(diff)
	w.StopTime()
	w.SetTime(18000)
	tw := &testWorld{t: t, w: w}
	t.Cleanup(func() { _ = w.Close() })
	return tw
}

func (w *testWorld) do(f func(tx *world.Tx)) {
	w.t.Helper()
	if _, err := world.Call(context.Background(), w.w, func(tx *world.Tx) (struct{}, error) {
		f(tx)
		return struct{}{}, nil
	}); err != nil {
		w.t.Fatal(err)
	}
}

func (w *testWorld) tick(n int) {
	for range n {
		w.w.AdvanceTick()
	}
}

func (w *testWorld) fill(a, b cube.Pos, blk world.Block) {
	w.do(func(tx *world.Tx) {
		for x := a[0]; x <= b[0]; x++ {
			for y := a[1]; y <= b[1]; y++ {
				for z := a[2]; z <= b[2]; z++ {
					tx.SetBlock(cube.Pos{x, y, z}, blk, nil)
				}
			}
		}
	})
}

// addPlayer adds a session-less survival player.
func (w *testWorld) addPlayer(pos mgl64.Vec3) *world.EntityHandle {
	var h *world.EntityHandle
	w.do(func(tx *world.Tx) {
		id := uuid.New()
		h = world.EntitySpawnOpts{Position: pos, ID: id}.New(player.Type, player.Config{
			Name: "tester", UUID: id, GameMode: world.GameModeSurvival, Position: pos,
		})
		tx.AddEntity(h)
	})
	return h
}

func (w *testWorld) player(tx *world.Tx, h *world.EntityHandle) *player.Player {
	e, ok := h.Entity(tx)
	if !ok {
		w.t.Fatal("player gone")
	}
	return e.(*player.Player)
}

// projectileOf returns the behaviour of a projectile handle, nil when gone.
func projectileOf(tx *world.Tx, h *world.EntityHandle) (*entity.Ent, *javaProjectile) {
	e, ok := h.Entity(tx)
	if !ok {
		return nil, nil
	}
	ent := e.(*entity.Ent)
	return ent, ent.Behaviour().(*javaProjectile)
}

var _ = block.Stone{}
