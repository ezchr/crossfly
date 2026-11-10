//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestFoxRaidsBerryBush: a fox walks to a ripe sweet berry bush (behavior.raid_garden) and raids it: the bush
// drops its berries and goes back to age 1 (SweetBerryBushBlock::executeEvent "minecraft:on_raided").
func TestFoxRaidsBerryBush(t *testing.T) {
	needs(t, true, true, append(walkers, "minecraft:behavior.raid_garden")...)
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	bush := cube.Pos{5, 1, 0}
	w.Do(func(tx *world.Tx) {
		w.Grass(tx, 0, 8)
		tx.SetBlock(bush, block.SweetBerryBush{Age: 3}, nil)
	})
	fox := w.Spawn("minecraft:fox", mgl64.Vec3{-3.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:ageable_grow_up"})
	if _, ok := w.TickUntil(600, func(tx *world.Tx) bool { return tx.Block(bush) == (block.SweetBerryBush{Age: 1}) }); !ok {
		t.Fatalf("the fox did not raid the bush:\n%v", w.Dump(fox))
	}
	if n := w.Count(isItem("sweet_berries")); n == 0 {
		// The fox may have picked them up already.
		fox.With(func(m *mob.Mob) {
			if h, _ := m.HeldItems(); h.Empty() {
				t.Error("raiding dropped no berries")
			}
		})
	}
}

// TestBerryBushHurtsButNotFoxes: a moving pig in a grown bush takes 1 contact damage of the bush; a fox's
// damage_sensor (is_block sweet_berry_bush, deals_damage no) keeps it unhurt.
func TestBerryBushHurtsButNotFoxes(t *testing.T) {
	needs(t, true, true, "minecraft:damage_sensor")
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	w.Do(func(tx *world.Tx) { w.Grass(tx, 0, 6) })
	pig := w.Spawn("minecraft:pig", mgl64.Vec3{0.5, 1, 0.5})
	fox := w.Spawn("minecraft:fox", mgl64.Vec3{3.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:ageable_grow_up"})
	w.Tick(1)
	hurt := func(r *mobtest.Ref) float64 {
		var lost float64
		w.Do(func(tx *world.Tx) {
			m, ok := r.Mob(tx)
			if !ok {
				t.Fatal("mob gone")
			}
			before := m.Health()
			m.SetVelocity(mgl64.Vec3{0.1, 0, 0})
			block.SweetBerryBush{Age: 2}.EntityInside(cube.PosFromVec3(m.Position()), tx, m)
			lost = before - m.Health()
			if v := m.Velocity(); v[0] > 0.081 {
				t.Errorf("the bush did not slow the mob: %v", v)
			}
		})
		return lost
	}
	if lost := hurt(pig); lost != 1 {
		t.Errorf("the pig lost %v health in the bush, want 1", lost)
	}
	if lost := hurt(fox); lost != 0 {
		t.Errorf("the fox lost %v health in the bush, want 0", lost)
	}
}

// TestFoxFollowsAndBreedsWithBerries: a fox follows a player holding sweet berries (behavior.tempt) and goes into
// love when fed them, glow berries too (breedable breed_items).
func TestFoxFollowsAndBreedsWithBerries(t *testing.T) {
	needs(t, true, true, append(walkers, "minecraft:behavior.tempt", "minecraft:breedable")...)
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	w.Do(func(tx *world.Tx) { w.Arena(tx, 12, 2) })
	p := w.AddPlayer("farmer", mgl64.Vec3{0.5, 1, 0.5})
	p.Hold(item.NewStack(block.SweetBerryBush{}, 16))
	fox := w.Spawn("minecraft:fox", mgl64.Vec3{8.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:ageable_grow_up"})
	if _, ok := w.TickUntil(400, func(tx *world.Tx) bool { return mobtest.HDistTx(tx, fox, p) < 3 }); !ok {
		t.Fatalf("the fox did not follow the berries:\n%v", w.Dump(fox))
	}
	if _, ok := p.Interact(fox, item.NewStack(block.SweetBerryBush{}, 1)); !ok {
		t.Fatalf("feeding sweet berries was not handled:\n%v", w.Dump(fox))
	}
	fox.With(func(m *mob.Mob) {
		if !m.InLove() {
			t.Error("a fox fed sweet berries is not in love")
		}
	})
}
