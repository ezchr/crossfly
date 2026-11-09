//go:build crossfly_full

package spawn

import (
	"math/rand/v2"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/data"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

var waterTypesOnce sync.Once

// waterTypes registers cod and squid if no other test did.
func waterTypes(t *testing.T) (cod, squid *mob.Type) {
	testTypes(t)
	waterTypesOnce.Do(func() {
		for _, name := range []string{"cod.json", "squid.json"} {
			raw, _ := data.Entity(name)
			d, err := mob.ParseDefinition(raw)
			if err != nil {
				panic(err)
			}
			if _, ok := mob.TypeFor(d.Identifier()); !ok {
				mob.RegisterType(d)
			}
		}
	})
	cod, _ = mob.TypeFor("minecraft:cod")
	squid, _ = mob.TypeFor("minecraft:squid")
	return cod, squid
}

// TestWaterSpawnDepth: WaterAnimal::checkSpawnRules moves a natural water
// spawn to 1-4 blocks under the top water block (Squid::checkSpawnRules
// 2-5), at the block corner, never below the block it started in; it needs
// a water source there.
func TestWaterSpawnDepth(t *testing.T) {
	cod, squid := waterTypes(t)
	w, _ := newWorld(t, nil)
	const bottom, top = floorY + 1, floorY + 12
	run(w, func(tx *world.Tx) {
		for x := -3; x <= 3; x++ {
			for z := -3; z <= 3; z++ {
				for y := bottom; y <= top; y++ {
					tx.SetBlock(cube.Pos{x, y, z}, block.Water{Still: true, Depth: 8}, nil)
				}
				// A one block deep pool beside it.
				tx.SetBlock(cube.Pos{x + 10, bottom, z}, block.Water{Still: true, Depth: 8}, nil)
			}
		}
		// Flowing water: no source.
		tx.SetBlock(cube.Pos{20, bottom, 0}, block.Water{Depth: 5}, nil)
	})
	run(w, func(tx *world.Tx) {
		e := &env{tx: tx, rnd: rand.New(rand.NewPCG(3, 4)), rng: tx.Range()}
		start := mgl64.Vec3{0.49, bottom, 0.49}
		for _, c := range []struct {
			t        *mob.Type
			f        func(*env, *mob.Type, mgl64.Vec3) (mgl64.Vec3, bool)
			min, max int
		}{{cod, waterAnimalSpawnRules, top - 4, top - 1}, {squid, squidSpawnRules, top - 5, top - 2}} {
			seen := map[int]bool{}
			for range 200 {
				pos, ok := c.f(e, c.t, start)
				if !ok {
					t.Fatalf("%v: refused at %v", c.t.EncodeEntity(), pos)
				}
				if pos[0] != 0 || pos[2] != 0 {
					t.Fatalf("%v: not at the block corner: %v", c.t.EncodeEntity(), pos)
				}
				y := int(pos[1])
				if y < c.min || y > c.max {
					t.Fatalf("%v: y %v, want %v..%v", c.t.EncodeEntity(), y, c.min, c.max)
				}
				seen[y] = true
			}
			if len(seen) != 4 {
				t.Fatalf("%v: depths %v, want 4", c.t.EncodeEntity(), seen)
			}
			// Shallow water: the mob stays in the block it started in.
			if pos, ok := c.f(e, c.t, mgl64.Vec3{10.49, bottom, 0.49}); !ok || int(pos[1]) != bottom {
				t.Fatalf("%v: shallow pool: %v %v", c.t.EncodeEntity(), pos, ok)
			}
			if _, ok := c.f(e, c.t, mgl64.Vec3{20.49, bottom, 0.49}); ok {
				t.Fatalf("%v: spawned in flowing water", c.t.EncodeEntity())
			}
		}
	})
}
