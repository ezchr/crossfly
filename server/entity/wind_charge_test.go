package entity_test

import (
	"context"
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func windWorld(t *testing.T) *world.World {
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func windBurst() block.WindBurst {
	return block.WindBurst{Power: 1.2, KnockbackScaling: 1.22, NegatesFallDamage: true, RandSource: rand.NewPCG(1, 2)}
}

// An entity right at the burst is pushed up by at most knockback_scaling; one more than 2.4 blocks away
// (twice the power) is not pushed.
func TestWindBurstPush(t *testing.T) {
	w := windWorld(t)
	_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		for x := -6; x <= 6; x++ {
			for z := -6; z <= 6; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		near := tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}, item.NewStack(item.Stick{}, 1)))
		far := tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{4.5, 64, 0.5}}, item.NewStack(item.Stick{}, 1)))
		windBurst().Burst(tx, mgl64.Vec3{0.5, 64.01, 0.5})

		v := near.(*entity.Ent).Velocity()
		if v[1] <= 0.5 || v[1] > 1.22+1e-9 || (mgl64.Vec2{v[0], v[2]}).Len() > 1e-6 {
			t.Errorf("entity on the burst pushed %v, want straight up, 0.5 < y <= 1.22", v)
		}
		if v := far.(*entity.Ent).Velocity(); v.Len() != 0 {
			t.Errorf("entity 4 blocks away pushed %v, want nothing", v)
		}
		return struct{}{}, nil
	})
}

// Wooden doors, trapdoors and levers in reach toggle; iron doors do not.
func TestWindBurstToggles(t *testing.T) {
	w := windWorld(t)
	_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		door := block.WoodDoor{Wood: block.OakWood(), Facing: cube.North}
		top := door
		top.Top = true
		tx.SetBlock(cube.Pos{1, 64, 0}, door, nil)
		tx.SetBlock(cube.Pos{1, 65, 0}, top, nil)
		iron := block.IronDoor{Facing: cube.North}
		ironTop := iron
		ironTop.Top = true
		tx.SetBlock(cube.Pos{-1, 64, 0}, iron, nil)
		tx.SetBlock(cube.Pos{-1, 65, 0}, ironTop, nil)
		tx.SetBlock(cube.Pos{0, 64, 1}, block.WoodTrapdoor{Wood: block.OakWood()}, nil)

		windBurst().Burst(tx, mgl64.Vec3{0.5, 64.5, 0.5})

		if d := tx.Block(cube.Pos{1, 64, 0}).(block.WoodDoor); !d.Open {
			t.Error("wooden door not opened")
		}
		if d := tx.Block(cube.Pos{1, 65, 0}).(block.WoodDoor); !d.Open {
			t.Error("top of the wooden door not opened with it")
		}
		if d := tx.Block(cube.Pos{-1, 64, 0}).(block.IronDoor); d.Open {
			t.Error("iron door opened")
		}
		if d := tx.Block(cube.Pos{0, 64, 1}).(block.WoodTrapdoor); !d.Open {
			t.Error("trapdoor not opened")
		}
		return struct{}{}, nil
	})
}

// A thrown wind charge flies straight at 1.5 blocks a tick and bursts on the ground.
func TestWindChargeFlight(t *testing.T) {
	w := windWorld(t)
	_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		for x := -2; x <= 12; x++ {
			tx.SetBlock(cube.Pos{x, 63, 0}, block.Stone{}, nil)
		}
		probe := tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{6.5, 64, 0.5}}, item.NewStack(item.Stick{}, 1)))
		h := entity.NewWindCharge(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 70, 0.5}, Velocity: mgl64.Vec3{1, -1, 0}.Normalize().Mul(1.5)}, nil)
		e := tx.AddEntity(h).(*entity.Ent)
		beh := e.Behaviour().(*entity.ProjectileBehaviour)
		for i := 0; i < 20; i++ {
			if _, ok := h.Entity(tx); !ok {
				break
			}
			beh.Tick(e, tx)
			if i == 0 {
				if v := e.Velocity(); v.Len() < 1.5-1e-9 || v.Len() > 1.5+1e-9 {
					t.Fatalf("wind charge slowed to %v after a tick", v.Len())
				}
			}
		}
		if _, ok := h.Entity(tx); ok {
			t.Fatal("wind charge did not burst on the ground")
		}
		if v := probe.(*entity.Ent).Velocity(); v.Len() == 0 {
			t.Error("entity next to the burst not pushed")
		}
		return struct{}{}, nil
	})
}
