//go:build crossfly_full

package mobtest_test

// Phase 1 end-to-end scenarios. They run with every mob package the
// vanilla aggregator links and skip (with the missing keys/packages named)
// while a package they need is not linked yet, so the suite stays green.
// Once everything is in, none of them should skip.
//
// Long scenarios (eggs, growing up, grazing: thousands of ticks) skip with
// -short.

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	_ "github.com/df-mc/dragonfly/server/mob/vanilla" // links every mob package
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// walkers are the keys every walking mob needs to move at all.
var walkers = []string{"minecraft:navigation.walk", "minecraft:movement.basic", "minecraft:jump.static"}

func needs(t *testing.T, defs, filters bool, keys ...string) {
	t.Helper()
	mobtest.Require(t, keys...)
	if defs {
		mobtest.RequireDefinitions(t)
	}
	if filters {
		mobtest.RequireFilters(t)
	}
}

func long(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("long scenario (-short)")
	}
}

// TestZombieReachesPlayerAcrossStairs: a zombie on the ground walks to a
// player standing on a 4-block platform whose only way up is a staircase.
func TestZombieReachesPlayerAcrossStairs(t *testing.T) {
	needs(t, false, true, append(walkers, "minecraft:behavior.nearest_attackable_target", "minecraft:behavior.melee_attack")...)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 16, block.Stone{})
		w.Platform(tx, cube.Pos{-3, 1, -14}, cube.Pos{3, 1, -8}, 4, block.Stone{}) // top y4, stand y5
		w.Stairs(tx, cube.Pos{0, 1, -4}, cube.North, 4, 2, block.Planks{}, block.Stone{})
	})
	p := w.AddPlayer("target", mgl64.Vec3{0.5, 5, -11.5})
	// 14.6 blocks away: zombie.json sets no minecraft:follow_range, so the
	// BDS default of 16 (Mob::_registerMobAttributes) bounds both the
	// target goal (TargetGoal::_withinRange) and the path search, which
	// only expands nodes within 16 blocks of the goal.
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{0.5, 1, 2.5}, mob.SpawnOptions{Event: "minecraft:as_adult"})

	n, ok := w.TickUntil(800, func(tx *world.Tx) bool {
		return (mobtest.HDistTx(tx, z, p) < 1.8 && z.PosTx(tx)[1] > 4.9) || p.HealthTx(tx) < 20
	})
	if !ok {
		t.Fatalf("zombie did not reach the player on the platform in %d ticks (dist %.1f):\n%v", n, w.Dist(z, p), w.Dump(z))
	}
	if y := z.Pos()[1]; y < 4.9 {
		t.Errorf("zombie hit the player from below (y %.2f): it never climbed the stairs", y)
	}
	t.Logf("reached after %d ticks", n)
}

// TestSkeletonKeepsDistanceAndShoots: a skeleton closes in to its
// ranged_attack range, stays out of melee range and shoots arrows.
func TestSkeletonKeepsDistanceAndShoots(t *testing.T) {
	needs(t, false, true, append(walkers, "minecraft:behavior.nearest_attackable_target", "minecraft:behavior.ranged_attack", "minecraft:shooter")...)
	var ranged struct {
		Range struct{ Max float64 } `json:"attack_range"`
	}
	mobtest.Decode(t, mobtest.Component(t, "skeleton.json", "", "minecraft:behavior.ranged_attack"), &ranged)
	maxRange := ranged.Range.Max // 15

	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Arena(tx, 26, 3) })
	p := w.AddPlayer("target", mgl64.Vec3{0.5, 1, -10.5})
	p.SetMaxHealth(500)
	// 15.5 blocks: inside the default follow range (16, the skeleton's
	// nearest_attackable_target has no max_dist), at the edge of its
	// 15-block attack range.
	s := w.Spawn("minecraft:skeleton", mgl64.Vec3{0.5, 1, 5})

	minDist, arrows, shotAt := math.MaxFloat64, 0, -1
	seen := map[*world.EntityHandle]bool{}
	w.Each(400, func(tx *world.Tx, tick int) bool {
		if tick > 40 { // let it notice the player first
			minDist = min(minDist, mobtest.HDistTx(tx, s, p))
		}
		for e := range tx.Entities() {
			if e.H().Type() == entity.ArrowType && !seen[e.H()] {
				seen[e.H()] = true
				arrows++
				if shotAt < 0 {
					shotAt = tick
				}
			}
		}
		return true
	})
	final := w.Dist(s, p)
	if arrows == 0 && p.Health() >= 500 {
		t.Fatalf("skeleton never shot (dist %.1f):\n%v", final, w.Dump(s))
	}
	// An arrow that hits knocks the player back up to two blocks, and the
	// skeleton then walks back into range: the 400th tick may fall in
	// between. It gets one attack interval (3 s) to be in range again.
	if _, ok := w.TickUntil(60, func(tx *world.Tx) bool { return mobtest.HDistTx(tx, s, p) <= maxRange+1 }); !ok {
		t.Errorf("skeleton stayed at %.1f blocks, outside its %.0f-block attack range", w.Dist(s, p), maxRange)
	}
	if minDist < 4 {
		t.Errorf("skeleton came within %.1f blocks: it should keep its distance", minDist)
	}
	t.Logf("%d arrows, first at tick %d, min dist %.1f, final %.1f, player health %.0f", arrows, shotAt, minDist, final, p.Health())
}

// TestCreeperSwellsAndExplodes: a creeper walks up to the player, starts
// swelling within behavior.swell's start_distance and explodes after the
// explode component's fuse.
func TestCreeperSwellsAndExplodes(t *testing.T) {
	needs(t, true, true, append(walkers, "minecraft:behavior.nearest_attackable_target", "minecraft:behavior.swell", "minecraft:explode")...)
	var swell struct {
		Start float64 `json:"start_distance"`
	}
	mobtest.Decode(t, mobtest.Component(t, "creeper.json", "", "minecraft:behavior.swell"), &swell)
	var explode struct {
		Fuse float64 `json:"fuse_length"`
	}
	mobtest.Decode(t, mobtest.Component(t, "creeper.json", "minecraft:exploding", "minecraft:explode"), &explode)

	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 16, block.Stone{}) })
	p := w.AddPlayer("target", mgl64.Vec3{0.5, 1, 0.5})
	c := w.Spawn("minecraft:creeper", mgl64.Vec3{0.5, 1, 10.5})

	swellTick, swellDist, goneTick := -1, 0.0, -1
	w.Each(600, func(tx *world.Tx, tick int) bool {
		m, ok := c.Mob(tx)
		if !ok || m.Dead() {
			goneTick = tick
			return false
		}
		if swellTick < 0 && (m.Ignited() || m.Swell() > 0 || m.HasComponent("minecraft:explode")) {
			swellTick, swellDist = tick, mobtest.DistTx(tx, c, p)
		}
		return true
	})
	if swellTick < 0 {
		t.Fatalf("creeper never started swelling (dist %.1f):\n%v", w.Dist(c, p), w.Dump(c))
	}
	// BDS SwellGoal and the target_nearby_sensor measure the gap between
	// the two boxes (AABB::distanceTo(AABB)): on flat ground that is the
	// feet distance minus half of each width (0.3 + 0.3).
	if gap := swellDist - 0.6; gap > swell.Start+0.3 {
		t.Errorf("creeper started swelling at a %.2f block gap, start_distance is %.1f", gap, swell.Start)
	}
	if goneTick < 0 {
		t.Fatalf("creeper started swelling at tick %d but never exploded:\n%v", swellTick, w.Dump(c))
	}
	fuse := int(explode.Fuse * 20)
	if d := goneTick - swellTick; d < fuse-2 || d > fuse+10 {
		t.Errorf("exploded %d ticks after it started swelling, fuse_length is %d ticks", d, fuse)
	}
	if h := p.Health(); h >= 20 {
		t.Errorf("explosion at %.1f blocks did not hurt the player", swellDist)
	}
	broken := 0
	w.Do(func(tx *world.Tx) {
		for x := -3; x <= 3; x++ {
			for z := -3; z <= 4; z++ {
				if _, ok := tx.Block(cube.Pos{x, 0, z}).(block.Air); ok {
					broken++
				}
			}
		}
	})
	if broken == 0 {
		t.Errorf("explosion broke no floor blocks")
	}
	t.Logf("swell at tick %d, %.2f blocks; exploded at tick %d; %d blocks broken", swellTick, swellDist, goneTick, broken)
}

// TestSpiderClimbsWall: a hostile spider climbs the side of a 5-block tower
// to reach the player on top (no other way up).
func TestSpiderClimbsWall(t *testing.T) {
	needs(t, true, true, "minecraft:navigation.climb", "minecraft:movement.basic", "minecraft:behavior.nearest_attackable_target")
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 14, block.Stone{})
		w.Platform(tx, cube.Pos{-2, 1, -2}, cube.Pos{2, 1, 2}, 5, block.Stone{}) // top y5, stand y6
	})
	p := w.AddPlayer("target", mgl64.Vec3{0.5, 6, 2.5})
	s := w.Spawn("minecraft:spider", mgl64.Vec3{0.5, 1, 10.5}, mob.SpawnOptions{Event: "minecraft:become_hostile"})

	climbed := false
	n, ok := w.TickUntil(800, func(tx *world.Tx) bool {
		if m, ok := s.Mob(tx); ok && m.Climbing() {
			climbed = true
		}
		return s.PosTx(tx)[1] > 5.9 && mobtest.HDistTx(tx, s, p) < 2.5
	})
	if !ok {
		t.Fatalf("spider did not get on top of the tower in %d ticks (pos %v):\n%v", n, s.Pos(), w.Dump(s))
	}
	if !climbed {
		t.Errorf("spider got up without ever having Climbing set (clients show no climbing)")
	}
	t.Logf("on top after %d ticks", n)
}

// TestCowsFollowWheat: cows walk to a player holding wheat (behavior.tempt).
func TestCowsFollowWheat(t *testing.T) {
	needs(t, false, false, append(walkers, "minecraft:behavior.tempt")...)
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	w.Do(func(tx *world.Tx) { w.Arena(tx, 12, 2) })
	p := w.AddPlayer("farmer", mgl64.Vec3{0.5, 1, 0.5})
	p.Hold(item.NewStack(item.Wheat{}, 16))
	cows := []*mobtest.Ref{
		w.Spawn("minecraft:cow", mgl64.Vec3{6.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_adult"}),
		w.Spawn("minecraft:cow", mgl64.Vec3{-5.5, 1, 3.5}, mob.SpawnOptions{Event: "minecraft:spawn_adult"}),
		w.Spawn("minecraft:cow", mgl64.Vec3{0.5, 1, -7.5}, mob.SpawnOptions{Event: "minecraft:spawn_adult"}),
	}
	n, ok := w.TickUntil(400, func(tx *world.Tx) bool {
		for _, c := range cows {
			if mobtest.HDistTx(tx, c, p) > 3 {
				return false
			}
		}
		return true
	})
	if !ok {
		for _, c := range cows {
			if d := w.Dist(c, p); d > 3 {
				t.Errorf("cow at %.1f blocks did not follow the wheat:\n%v", d, w.Dump(c))
			}
		}
		return
	}
	t.Logf("all cows close after %d ticks", n)
}

// TestSheepEatsGrassAndRegrowsWool: a sheared sheep eats grass (grass to
// dirt) and gets its wool back (on_eat_block removes sheep_sheared).
func TestSheepEatsGrassAndRegrowsWool(t *testing.T) {
	needs(t, true, true, append(walkers, "minecraft:behavior.eat_block", "minecraft:interact")...)
	long(t)
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	w.Do(func(tx *world.Tx) {
		w.Grass(tx, 0, 5)
		w.Wall(tx, cube.Pos{-6, 1, -6}, cube.Pos{6, 1, -6}, 2, block.Glass{})
		w.Wall(tx, cube.Pos{-6, 1, 6}, cube.Pos{6, 1, 6}, 2, block.Glass{})
		w.Wall(tx, cube.Pos{-6, 1, -6}, cube.Pos{-6, 1, 6}, 2, block.Glass{})
		w.Wall(tx, cube.Pos{6, 1, -6}, cube.Pos{6, 1, 6}, 2, block.Glass{})
	})
	p := w.AddPlayer("shepherd", mgl64.Vec3{4.5, 1, 4.5})
	sh := w.Spawn("minecraft:sheep", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "spawn_adult"})
	w.Tick(1)
	if _, ok := p.Interact(sh, item.NewStack(item.Shears{}, 1)); !ok {
		t.Fatalf("shearing was not handled:\n%v", w.Dump(sh))
	}
	sh.With(func(m *mob.Mob) {
		if !m.HasComponent("minecraft:is_sheared") || !m.Sheared() {
			t.Fatalf("sheep not sheared after shearing (is_sheared %v, meta %v)", m.HasComponent("minecraft:is_sheared"), m.Sheared())
		}
	})
	if n := w.Count(isItem("wool")); n == 0 {
		t.Errorf("shearing dropped no wool")
	}
	// success_chance 0.001/tick for adults: 6000 ticks miss with p = e^-6.
	n, ok := w.TickUntil(6000, func(tx *world.Tx) bool {
		m, ok := sh.Mob(tx)
		return ok && !m.HasComponent("minecraft:is_sheared")
	})
	if !ok {
		t.Fatalf("sheep did not regrow its wool in %d ticks:\n%v", n, w.Dump(sh))
	}
	sh.With(func(m *mob.Mob) {
		if m.Sheared() {
			t.Errorf("wool regrown but Sheared still set for clients")
		}
	})
	dirt := 0
	w.Do(func(tx *world.Tx) {
		for x := -5; x <= 5; x++ {
			for z := -5; z <= 5; z++ {
				if _, ok := tx.Block(cube.Pos{x, 0, z}).(block.Dirt); ok {
					dirt++
				}
			}
		}
	})
	if dirt == 0 {
		t.Errorf("wool regrew but no grass turned to dirt")
	}
	t.Logf("ate and regrew after %d ticks", n)
}

// TestChickenLaysEggs: an adult chicken drops an egg within
// spawn_entity's max_wait_time (600 s).
func TestChickenLaysEggs(t *testing.T) {
	needs(t, true, true, append(walkers, "minecraft:spawn_entity")...)
	long(t)
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	w.Do(func(tx *world.Tx) { w.Arena(tx, 6, 2) })
	w.Spawn("minecraft:chicken", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_adult"})
	egg := isItem("egg")
	n, ok := w.TickUntil(600*20+40, func(tx *world.Tx) bool {
		for e := range tx.Entities() {
			if egg(e) {
				return true
			}
		}
		return false
	})
	if !ok {
		t.Fatalf("no egg in %d ticks:\n%v", n, w.Dump(w.Mobs("minecraft:chicken")[0]))
	}
	if n < 300*20-1 {
		t.Errorf("egg after %d ticks, before min_wait_time (300 s)", n)
	}
	t.Logf("egg after %d ticks", n)
}

// TestMobsSurviveSaveReload: mobs saved and loaded keep their component
// groups (active components), properties, health and position.
func TestMobsSurviveSaveReload(t *testing.T) {
	needs(t, true, false)
	w := mobtest.New(t, mobtest.Options{Persistent: true})
	w.Do(func(tx *world.Tx) { w.Arena(tx, 10, 2) })
	refs := []*mobtest.Ref{
		w.Spawn("minecraft:sheep", mgl64.Vec3{3.5, 1, 3.5}, mob.SpawnOptions{Event: "spawn_adult"}),
		w.Spawn("minecraft:cow", mgl64.Vec3{-3.5, 1, 3.5}, mob.SpawnOptions{Event: "minecraft:entity_born"}),
		w.Spawn("minecraft:zombie", mgl64.Vec3{3.5, 1, -3.5}),
		w.Spawn("minecraft:creeper", mgl64.Vec3{-3.5, 1, -3.5}),
	}
	refs[0].With(func(m *mob.Mob) {
		m.Fire("minecraft:on_sheared", nil)
		if !m.HasComponent("minecraft:is_sheared") {
			t.Fatalf("on_sheared did not add sheep_sheared")
		}
	})
	refs[1].With(func(m *mob.Mob) {
		if !m.HasComponent("minecraft:is_baby") {
			t.Fatalf("entity_born did not make a baby cow")
		}
	})
	refs[3].With(func(m *mob.Mob) { m.SetHealth(7) })
	w.Tick(10)

	type snap struct {
		comps []string
		props map[string]any
		pos   mgl64.Vec3
		hp    float64
	}
	take := func() []snap {
		out := make([]snap, len(refs))
		for i, r := range refs {
			r.With(func(m *mob.Mob) {
				s := snap{comps: slices.Sorted(slices.Values(m.Components())), props: map[string]any{}, pos: m.Pos(), hp: m.Health()}
				for _, p := range m.Properties() {
					s.props[p.Decl.Name] = p.Value
				}
				out[i] = s
			})
		}
		return out
	}
	before := take()
	w.Reload()
	for _, r := range refs {
		if r.H() == nil {
			t.Fatalf("mob %v missing after reload", r.UUID())
		}
	}
	w.Tick(1)
	after := take()
	for i := range refs {
		b, a := before[i], after[i]
		if !slices.Equal(b.comps, a.comps) {
			t.Errorf("mob %d components changed over reload:\n before %v\n after  %v", i, b.comps, a.comps)
		}
		for k, v := range b.props {
			if a.props[k] != v {
				t.Errorf("mob %d property %v: %v -> %v", i, k, v, a.props[k])
			}
		}
		if b.hp != a.hp {
			t.Errorf("mob %d health %v -> %v", i, b.hp, a.hp)
		}
		if d := a.pos.Sub(b.pos).Len(); d > 1 {
			t.Errorf("mob %d moved %.2f blocks over the reload", i, d)
		}
	}
}

// TestBabyGrowsUp: a baby cow grows up after ageable.duration, and faster
// when fed its feed_items.
func TestBabyGrowsUp(t *testing.T) {
	needs(t, true, false, "minecraft:ageable")
	var ageable struct {
		Duration float64 `json:"duration"`
	}
	mobtest.Decode(t, mobtest.Component(t, "cow.json", "minecraft:cow_baby", "minecraft:ageable"), &ageable)

	t.Run("fed", func(t *testing.T) {
		w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
		w.Do(func(tx *world.Tx) { w.Arena(tx, 6, 2) })
		p := w.AddPlayer("farmer", mgl64.Vec3{3.5, 1, 3.5})
		c := w.Spawn("minecraft:cow", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:entity_born"})
		for i := range 10 { // feed_items grow 10% of the duration each
			if _, ok := p.Interact(c, item.NewStack(item.Wheat{}, 1)); !ok {
				t.Fatalf("feeding %d was not handled:\n%v", i, w.Dump(c))
			}
			w.Tick(1)
		}
		if _, ok := w.TickUntil(40, func(tx *world.Tx) bool { m, ok := c.Mob(tx); return ok && !m.HasComponent("minecraft:is_baby") }); !ok {
			t.Fatalf("10 wheat did not grow the calf up:\n%v", w.Dump(c))
		}
	})
	t.Run("time", func(t *testing.T) {
		long(t)
		w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
		w.Do(func(tx *world.Tx) { w.Arena(tx, 6, 2) })
		c := w.Spawn("minecraft:cow", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:entity_born"})
		c.With(func(m *mob.Mob) {
			if !m.Baby() {
				t.Errorf("baby cow not Baby for clients")
			}
		})
		ticks := int(ageable.Duration * 20)
		n, ok := w.TickUntil(ticks+40, func(tx *world.Tx) bool {
			m, ok := c.Mob(tx)
			return ok && !m.HasComponent("minecraft:is_baby")
		})
		if !ok {
			t.Fatalf("calf not grown after %d ticks:\n%v", n, w.Dump(c))
		}
		if n < ticks-20 {
			t.Errorf("grew up after %d ticks, duration is %d", n, ticks)
		}
		c.With(func(m *mob.Mob) {
			if m.Baby() {
				t.Errorf("grown cow still Baby for clients")
			}
		})
	})
}

// isItem matches dropped item entities whose item name ends in suffix
// ("egg" also matches brown/blue eggs, "wool" every colour).
func isItem(suffix string) func(e world.Entity) bool {
	return func(e world.Entity) bool {
		ent, ok := e.(*entity.Ent)
		if !ok {
			return false
		}
		b, ok := ent.Behaviour().(*entity.ItemBehaviour)
		if !ok {
			return false
		}
		name, _ := b.Item().Item().EncodeItem()
		return strings.HasSuffix(name, suffix)
	}
}
