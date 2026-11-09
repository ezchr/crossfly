//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	vc "github.com/df-mc/dragonfly/server/mob/component/village"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func init() { item.RegisterMobItems() } // the lead

func filterTest(t *testing.T, raw string, ctx *mob.FilterContext) bool {
	t.Helper()
	f, err := mob.CompileFilter([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return f.Test(ctx)
}

// TestTraderLlamasLeashed: a wandering trader spawns its two trader llamas
// leashed to it (its minecraft:spawn_entity, should_leash); is_leashed keeps
// them persistent, and a llama let go loses its persistence
// (llama_wandering_trader's environment_sensor), while is_leashed_to tells
// the trader its llamas.
func TestTraderLlamasLeashed(t *testing.T) {
	mobtest.RequireFilters(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 12, block.Stone{}) })
	trader := w.Spawn("minecraft:wandering_trader", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(3)
	llamas := w.Mobs("minecraft:trader_llama")
	if len(llamas) != 2 {
		t.Fatalf("%d trader llamas, want 2", len(llamas))
	}
	w.Tick(40)
	w.Do(func(tx *world.Tx) {
		tm, ok := trader.Mob(tx)
		if !ok {
			t.Fatal("trader gone")
		}
		for _, r := range llamas {
			m, ok := r.Mob(tx)
			if !ok {
				t.Fatal("llama gone")
			}
			h, ok := core.LeashHolder(m)
			if !ok || h.H() != tm.H() {
				t.Fatalf("llama not leashed to the trader (%v)", ok)
			}
			if !m.HasComponent("minecraft:persistent") {
				t.Fatalf("leashed trader llama lost its persistence: %v", m.Components())
			}
			if !filterTest(t, `{"test":"is_leashed","value":true}`, m.Ctx()) {
				t.Fatal("is_leashed false for a leashed llama")
			}
			ctx := tm.Ctx()
			ctx.Other = m
			if !filterTest(t, `{"test":"is_leashed_to","subject":"other","value":true}`, ctx) {
				t.Fatal("is_leashed_to false for the trader's llama")
			}
		}
		m, _ := llamas[0].Mob(tx)
		core.DropLeash(m, false, false)
		if filterTest(t, `{"test":"is_leashed","value":true}`, m.Ctx()) {
			t.Fatal("is_leashed true after the leash was dropped")
		}
	})
	w.Tick(5)
	w.Do(func(tx *world.Tx) {
		m0, _ := llamas[0].Mob(tx)
		m1, _ := llamas[1].Mob(tx)
		if m0.HasComponent("minecraft:persistent") {
			t.Error("trader llama let go kept its persistence")
		}
		if !m1.HasComponent("minecraft:persistent") {
			t.Error("leashed trader llama lost its persistence")
		}
	})
}

// TestLeashSavedWithKnot: a cow tied to a fence knot is still tied to it
// after the world is saved and loaded again (LeasherID: the knot's UUID; the
// knot is saved with the world).
func TestLeashSavedWithKnot(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{Persistent: true})
	fence := cube.Pos{3, 1, 0}
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 8, block.Stone{})
		tx.SetBlock(fence, block.WoodFence{Wood: block.OakWood()}, nil)
	})
	cow := w.Spawn("minecraft:cow", mgl64.Vec3{0.5, 1, 0.5})
	p := w.AddPlayer("p", mgl64.Vec3{1.5, 1, 0.5})
	w.Tick(1)
	if _, ok := p.Interact(cow, item.NewStack(item.Lead{}, 1)); !ok {
		t.Fatal("lead refused by the cow")
	}
	p.With(func(pl *player.Player) {
		if !core.BindPlayerMobs(pl.Tx(), pl, fence) {
			t.Fatal("cow not tied to the fence")
		}
	})
	w.Tick(2)
	w.Reload()
	w.Tick(3)
	w.Do(func(tx *world.Tx) {
		m, ok := cow.Mob(tx)
		if !ok {
			t.Fatal("cow gone after reload")
		}
		h, ok := core.LeashHolder(m)
		if !ok {
			t.Fatal("cow not leashed after reload")
		}
		k, ok := h.(*core.Knot)
		if !ok || cube.PosFromVec3(k.Position()) != fence {
			t.Fatalf("cow leashed to %T after reload, want the knot at %v", h, fence)
		}
	})
}

// TestLeadOnHorse: a lead used on a wild horse or llama leashes it.
func TestLeadOnHorse(t *testing.T) {
	for _, id := range []string{"minecraft:horse", "minecraft:llama", "minecraft:pig", "minecraft:wolf"} {
		t.Run(id, func(t *testing.T) {
			w := mobtest.New(t, mobtest.Options{})
			w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 6, block.Stone{}) })
			r := w.Spawn(id, mgl64.Vec3{0.5, 1, 2.5}, mob.SpawnOptions{Event: "minecraft:entity_spawned"})
			p := w.AddPlayer("p", mgl64.Vec3{0.5, 1, 0.5})
			w.Tick(2)
			if _, ok := p.Interact(r, item.NewStack(item.Lead{}, 1)); !ok {
				t.Fatal("lead refused")
			}
			w.Do(func(tx *world.Tx) {
				m, _ := r.Mob(tx)
				if !core.Leashed(m) {
					t.Fatalf("not leashed; components %v", m.Components())
				}
			})
			p.With(func(pl *player.Player) {
				if _, riding := pl.RidingEntity(pl.Tx()); riding {
					t.Fatal("the lead mounted the player")
				}
			})
		})
	}
}

// TestLlamaCaravan: llamas near a leashed llama follow it in a caravan
// (follow_caravan, which the leashed one loses through on_leash).
func TestLlamaCaravan(t *testing.T) {
	mobtest.Require(t, "minecraft:behavior.follow_caravan")
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 16, block.Stone{}) })
	lead := w.Spawn("minecraft:llama", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_adult"})
	var others []*mobtest.Ref
	for i := 0; i < 3; i++ {
		others = append(others, w.Spawn("minecraft:llama", mgl64.Vec3{4.5 + float64(i), 1, 3.5}, mob.SpawnOptions{Event: "minecraft:spawn_adult"}))
	}
	p := w.AddPlayer("p", mgl64.Vec3{-1.5, 1, 0.5})
	w.Tick(1)
	p.With(func(pl *player.Player) {
		m, _ := lead.Mob(pl.Tx())
		core.Leash(m, pl)
	})
	_, ok := w.TickUntil(400, func(tx *world.Tx) bool {
		n := 0
		for _, r := range others {
			if m, ok := r.Mob(tx); ok {
				if _, in := vc.CaravanHead(m); in {
					n++
				}
			}
		}
		return n >= 2
	})
	if !ok {
		t.Fatal("llamas did not join the leashed llama's caravan")
	}
	w.Do(func(tx *world.Tx) {
		m, _ := lead.Mob(tx)
		if _, ok := m.Goals().Goal("minecraft:behavior.follow_caravan"); ok {
			t.Error("the leashed llama kept follow_caravan")
		}
	})
}
