//go:build crossfly_full

package animal_test

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/animal"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	_ "github.com/df-mc/dragonfly/server/mob/vanilla"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

func build(t *testing.T, key, raw string) mob.Goal {
	t.Helper()
	spec, err := mob.ParseGoalSpec(key, json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	g, err := animal.Factories[key](spec)
	if err != nil {
		t.Fatalf("%v: %v", key, err)
	}
	return g
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-4 }

func checkParams(t *testing.T, key string, got map[string]float64, want map[string]float64) {
	t.Helper()
	for k, v := range want {
		if g, ok := got[k]; !ok || !near(g, v) {
			t.Errorf("%v: %v = %v, want %v", key, k, g, v)
		}
	}
}

// TestBDSDefaults builds every goal from {} and checks the BDS defaults
// (LegacyGoalDefinition::parse, <Goal>::Definition constructors).
func TestBDSDefaults(t *testing.T) {
	want := map[string]map[string]float64{
		"minecraft:behavior.stalk_and_pounce_on_target": {"stalk_speed": 1.2, "max_stalk_dist": 10, "leap_height": 0.9, "leap_dist": 0.8, "pounce_max_dist": 5, "strike_dist": 2, "interest_ticks": 40, "stuck_ticks": 40},
		"minecraft:behavior.nap":                        {"cooldown_min": 0, "cooldown_max": 0, "mob_detect_dist": 6, "mob_detect_height": 6},
		"minecraft:behavior.find_cover":                 {"speed_multiplier": 1, "cooldown_ticks": 0},
		"minecraft:behavior.eat_carried_item":           {"delay_ticks": 0},
		"minecraft:behavior.random_look_around_and_sit": {"min_look_count": 1, "max_look_count": 2, "min_look_time": 20, "max_look_time": 40, "min_angle": -180, "max_angle": 180, "probability": 0.02, "cooldown_ticks": 0},
		"minecraft:behavior.raid_garden":                {"search_range": 0, "search_height": 1, "goal_radius": 0.5, "speed_multiplier": 1, "max_to_eat": 6, "initial_eat_delay_ticks": 0, "eat_delay_ticks": 40, "full_delay_ticks": 2000},
		"minecraft:behavior.defend_trusted_target":      {"within_radius": 0, "must_see": 0},
		"minecraft:behavior.roll":                       {"probability": 0.02},
		"minecraft:behavior.sneeze":                     {"probability": 0.02, "cooldown_ticks": 0, "prepare_ticks": 20, "drop_item_chance": 1, "within_radius": 0},
		"minecraft:behavior.snacking":                   {"cooldown_ticks": 150, "cooldown_min_ticks": 10, "stop_chance": 0.0016666667},
		"minecraft:behavior.lay_down":                   {"interval": 120, "random_stop_interval": 120},
		"minecraft:behavior.random_sitting":             {"start_chance": 0.1, "stop_chance": 0.3, "cooldown_ticks": 2000, "min_sit_ticks": 200},
		"minecraft:behavior.croak":                      {"duration_min": 4.5, "duration_max": 4.5, "interval_min": 10, "interval_max": 20},
		"minecraft:behavior.eat_mob":                    {"run_speed": 1, "eat_animation_ticks": 20, "pull_in_force": 1, "reach_mob_distance": 1},
		"minecraft:behavior.jump_to_block":              {"search_width": 8, "search_height": 10, "minimum_path_length": 5, "minimum_distance": 2, "scale_factor": 0.7, "max_velocity": 1.5, "cooldown_min": 10, "cooldown_max": 20, "preferred_blocks_chance": 1},
		"minecraft:behavior.lay_egg":                    {"search_range": 0, "search_height": 1, "goal_radius": 0.5, "speed_multiplier": 1, "lay_ticks": 200, "use_default_animation": 1, "allow_laying_from_below": 0},
		"minecraft:behavior.random_search_and_dig":      {"speed_multiplier": 1, "retries": 10, "goal_radius": 1.5, "search_range_xz": 20, "search_range_y": 3},
	}
	for key := range animal.Factories {
		g := build(t, key, `{}`)
		if animal.Params(g) == nil {
			t.Errorf("%v: no params", key)
		}
		if w, ok := want[key]; ok {
			checkParams(t, key, animal.Params(g), w)
		}
	}
	if f := build(t, "minecraft:behavior.scared", `{}`).Flags(); f != mob.FlagMove|mob.FlagLook {
		t.Errorf("scared flags %v, want move|look (BDS 3)", f)
	}
	if f := build(t, "minecraft:behavior.eat_mob", `{}`).Flags(); f != mob.FlagMove|mob.FlagLook {
		t.Errorf("eat_mob flags %v, want move|look (BDS 3)", f)
	}
}

// TestVanillaJSON builds the goals from the vanilla 1.26 definitions.
func TestVanillaJSON(t *testing.T) {
	cases := []struct {
		file, group, key string
		want             map[string]float64
	}{
		{"fox.json", "", "minecraft:behavior.stalk_and_pounce_on_target", map[string]float64{"max_stalk_dist": 12}},
		{"fox.json", "minecraft:fox_day", "minecraft:behavior.nap", map[string]float64{"cooldown_min": 2, "cooldown_max": 7, "mob_detect_dist": 12}},
		{"fox.json", "minecraft:fox_day", "minecraft:behavior.find_cover", map[string]float64{"cooldown_ticks": 100}},
		{"fox.json", "", "minecraft:behavior.eat_carried_item", map[string]float64{"delay_ticks": 560}},
		{"camel.json", "", "minecraft:behavior.random_look_around_and_sit", map[string]float64{"min_look_count": 2, "max_look_count": 5, "min_look_time": 80, "max_look_time": 100, "min_angle": -30, "max_angle": 30, "probability": 0.001, "cooldown_ticks": 100}},
		{"rabbit.json", "", "minecraft:behavior.raid_garden", map[string]float64{"search_range": 16, "goal_radius": 1, "speed_multiplier": 0.6}},
		{"fox.json", "minecraft:trusting_fox", "minecraft:behavior.defend_trusted_target", map[string]float64{"within_radius": 25}},
		{"panda.json", "minecraft:panda_playful", "minecraft:behavior.roll", map[string]float64{"probability": 0.013}},
		{"panda.json", "minecraft:panda_sneezing", "minecraft:behavior.sneeze", map[string]float64{"probability": 0.002, "cooldown_ticks": 20, "within_radius": 10, "drop_item_chance": 0.001}},
		{"panda.json", "", "minecraft:behavior.snacking", map[string]float64{"cooldown_ticks": 450, "cooldown_min_ticks": 400}},
		{"panda.json", "minecraft:panda_lazy", "minecraft:behavior.lay_down", map[string]float64{"interval": 400, "random_stop_interval": 2000}},
		{"panda.json", "", "minecraft:behavior.random_sitting", map[string]float64{"start_chance": 0.01, "cooldown_ticks": 600, "min_sit_ticks": 200}},
		{"frog.json", "", "minecraft:behavior.croak", map[string]float64{"duration_min": 4.5, "interval_min": 10, "interval_max": 20}},
		{"frog.json", "", "minecraft:behavior.eat_mob", map[string]float64{"run_speed": 2, "eat_animation_ticks": 6, "pull_in_force": 0.75, "reach_mob_distance": 1.75}},
		{"frog.json", "", "minecraft:behavior.jump_to_block", map[string]float64{"minimum_path_length": 2, "minimum_distance": 1, "max_velocity": 1, "cooldown_min": 5, "cooldown_max": 7, "preferred_blocks_chance": 0.5, "scale_factor": 0.6}},
		{"goat.json", "", "minecraft:behavior.jump_to_block", map[string]float64{"search_width": 10, "minimum_path_length": 8, "cooldown_min": 30, "cooldown_max": 60}},
		{"frog.json", "pregnant", "minecraft:behavior.lay_egg", map[string]float64{"lay_ticks": 40, "allow_laying_from_below": 1, "use_default_animation": 0, "search_range": 10, "goal_radius": 1.7}},
		{"turtle.json", "minecraft:wants_to_lay_egg", "minecraft:behavior.lay_egg", map[string]float64{"search_range": 16, "search_height": 4, "goal_radius": 1.5, "lay_ticks": 200}},
		{"sniffer.json", "sniffer_search_and_dig", "minecraft:behavior.random_search_and_dig", map[string]float64{"speed_multiplier": 1.25, "retries": 5, "goal_radius": 2, "dig_min": 8, "dig_max": 10, "item_after_ticks": 120, "item_offset": 2.25}},
	}
	for _, c := range cases {
		raw := mobtest.Component(t, c.file, c.group, c.key)
		g := build(t, c.key, string(raw))
		checkParams(t, c.file+" "+c.key, animal.Params(g), c.want)
	}
}

// arena spawns id at the centre of a stone arena and replaces its goals by
// key built from raw (priority 0).
func arena(t *testing.T, opts mobtest.Options, id, key, raw string) (*mobtest.World, *mobtest.Ref) {
	t.Helper()
	w := mobtest.New(t, opts)
	w.Do(func(tx *world.Tx) { w.Arena(tx, 12, 3) })
	r := w.Spawn(id, mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(3) // the first ticks' events re-add the JSON goals
	install(t, w, r, key, raw)
	return w, r
}

func install(t *testing.T, w *mobtest.World, r *mobtest.Ref, key, raw string) {
	t.Helper()
	g := build(t, key, raw)
	r.With(func(m *mob.Mob) {
		m.Goals().ClearAll()
		m.Goals().Add(key, 0, g)
	})
}

func flag(w *mobtest.World, r *mobtest.Ref, f uint8) bool {
	v := false
	r.With(func(m *mob.Mob) { v = m.ActorFlag(f) })
	return v
}

func meta(w *mobtest.World, r *mobtest.Ref) mob.Meta {
	var v mob.Meta
	r.With(func(m *mob.Mob) { v = m.Meta() })
	return v
}

func until(t *testing.T, w *mobtest.World, r *mobtest.Ref, n int, what string, cond func(m *mob.Mob) bool) {
	t.Helper()
	if _, ok := w.TickUntil(n, func(tx *world.Tx) bool {
		m, ok := r.Mob(tx)
		return ok && cond(m)
	}); !ok {
		t.Fatalf("%v not seen in %d ticks:\n%v", what, n, w.Dump(r))
	}
}

func TestRoll(t *testing.T) {
	w, p := arena(t, mobtest.Options{}, "minecraft:panda", "minecraft:behavior.roll", `{"probability":1}`)
	w.Tick(5)
	start := p.Pos()
	until(t, w, p, 40, "rolling flag", func(m *mob.Mob) bool { return m.ActorFlag(protocol.EntityDataFlagRolling) })
	w.Tick(20)
	if d := p.Pos().Sub(start); math.Hypot(d[0], d[2]) < 0.5 {
		t.Errorf("panda rolled only %.2f blocks", math.Hypot(d[0], d[2]))
	}
	until(t, w, p, 60, "roll end", func(m *mob.Mob) bool { return !m.ActorFlag(protocol.EntityDataFlagRolling) })
}

func TestSneeze(t *testing.T) {
	w, p := arena(t, mobtest.Options{}, "minecraft:panda", "minecraft:behavior.sneeze",
		`{"probability":1,"prepare_time":0.25,"within_radius":10,"cooldown_time":100,"entity_types":[{"filters":{"test":"is_family","subject":"other","value":"panda"}}]}`)
	other := w.Spawn("minecraft:panda", mgl64.Vec3{3.5, 1, 0.5})
	other.With(func(m *mob.Mob) { m.Goals().ClearAll() })
	until(t, w, p, 5, "sneezing flag", func(m *mob.Mob) bool { return m.ActorFlag(protocol.EntityDataFlagSneezing) })
	maxY := 0.0
	w.Each(20, func(tx *world.Tx, _ int) bool { maxY = math.Max(maxY, other.PosTx(tx)[1]); return true })
	if maxY < 1.2 {
		t.Errorf("nearby panda did not jump at the sneeze (max y %.2f)", maxY)
	}
	if flag(w, p, protocol.EntityDataFlagSneezing) {
		t.Error("still sneezing after the sneeze")
	}
}

func TestSnacking(t *testing.T) {
	w, p := arena(t, mobtest.Options{}, "minecraft:panda", "minecraft:behavior.snacking",
		`{"snacking_stop_chance":0,"items":[{"name":"minecraft:bamboo"}]}`)
	w.Do(func(tx *world.Tx) {
		tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{3.5, 1, 0.5}}, item.NewStack(block.Bamboo{}, 3)))
	})
	until(t, w, p, 200, "panda eating bamboo", func(m *mob.Mob) bool {
		main, _ := m.HeldItems()
		return !main.Empty() && m.Sitting() && m.ActorFlag(protocol.EntityDataFlagEating)
	})
	n := w.Count(func(e world.Entity) bool { return e.H().Type() == entity.ItemType })
	if n != 1 {
		t.Errorf("%d item entities left, want 1 (the 2 bamboo left over)", n)
	}
}

func TestLayDown(t *testing.T) {
	w, p := arena(t, mobtest.Options{}, "minecraft:panda", "minecraft:behavior.lay_down", `{"interval":0,"random_stop_interval":0}`)
	// random_stop_interval 0 stops at once: the flag goes on and off.
	seen := false
	w.Each(5, func(tx *world.Tx, _ int) bool {
		m, _ := p.Mob(tx)
		seen = seen || m.ActorFlag(protocol.EntityDataFlagLayingDown)
		return true
	})
	install(t, w, p, "minecraft:behavior.lay_down", `{"interval":0,"random_stop_interval":100000}`)
	until(t, w, p, 5, "laying down", func(m *mob.Mob) bool { return m.ActorFlag(protocol.EntityDataFlagLayingDown) })
	_ = seen
}

func TestScared(t *testing.T) {
	w, p := arena(t, mobtest.Options{Time: mobtest.Noon}, "minecraft:panda", "minecraft:behavior.scared", `{}`)
	w.Tick(3)
	if flag(w, p, protocol.EntityDataFlagScared) {
		t.Fatal("scared without a storm")
	}
	w.W.StartRaining(time.Minute)
	w.W.StartThundering(time.Minute)
	until(t, w, p, 10, "scared in a thunderstorm", func(m *mob.Mob) bool { return m.ActorFlag(protocol.EntityDataFlagScared) })
	w.W.StopThundering()
	until(t, w, p, 10, "calm after the storm", func(m *mob.Mob) bool { return !m.ActorFlag(protocol.EntityDataFlagScared) })
}

func TestRandomSitting(t *testing.T) {
	w, p := arena(t, mobtest.Options{}, "minecraft:panda", "minecraft:behavior.random_sitting", `{"start_chance":1,"stop_chance":1,"min_sit_time":1,"cooldown":10}`)
	until(t, w, p, 20, "sitting", func(m *mob.Mob) bool { return m.Sitting() })
	n, _ := w.TickUntil(60, func(tx *world.Tx) bool { m, _ := p.Mob(tx); return !m.Sitting() })
	if n < 18 {
		t.Errorf("stood up after %d ticks, before min_sit_time (20)", n)
	}
	if meta(w, p).Sitting {
		t.Error("did not stand up after min_sit_time with stop_chance 1")
	}
}

func TestNap(t *testing.T) {
	w, f := arena(t, mobtest.Options{}, "minecraft:fox", "minecraft:behavior.nap", `{"mob_detect_dist":6,"wake_mob_exceptions":{"test":"is_family","subject":"other","value":"fox"}}`)
	until(t, w, f, 20, "napping", func(m *mob.Mob) bool { return m.ActorFlag(protocol.EntityDataFlagSleeping) })
	w.AddPlayer("waker", mgl64.Vec3{3.5, 1, 0.5})
	until(t, w, f, 5, "woken by a player", func(m *mob.Mob) bool { return !m.ActorFlag(protocol.EntityDataFlagSleeping) })
}

func TestStalkAndPounce(t *testing.T) {
	w, f := arena(t, mobtest.Options{}, "minecraft:fox", "minecraft:behavior.stalk_and_pounce_on_target", `{"interest_time":0.25}`)
	c := w.Spawn("minecraft:chicken", mgl64.Vec3{7.5, 1, 0.5})
	c.With(func(m *mob.Mob) { m.Goals().ClearAll(); m.SetMaxHealth(100); m.SetHealth(100) })
	w.Do(func(tx *world.Tx) {
		fm, _ := f.Mob(tx)
		cm, _ := c.Mob(tx)
		fm.SetTarget(cm)
	})
	until(t, w, f, 20, "stalking", func(m *mob.Mob) bool { return m.ActorFlag(protocol.EntityDataFlagStalking) })
	until(t, w, f, 200, "interested before the pounce", func(m *mob.Mob) bool { return m.ActorFlag(protocol.EntityDataFlagInterested) })
	hurt := false
	w.Each(80, func(tx *world.Tx, _ int) bool {
		cm, _ := c.Mob(tx)
		hurt = cm.Health() < 100
		return !hurt
	})
	if !hurt {
		t.Fatalf("the pounce never hit the chicken:\n%v", w.Dump(f))
	}
}

func TestFindCover(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	w.Do(func(tx *world.Tx) {
		w.Arena(tx, 12, 0)
		// A lit, roofed shelter west of the fox.
		w.Fill(tx, cube.Pos{-9, 4, -3}, cube.Pos{-4, 4, 3}, block.Stone{})
		w.Fill(tx, cube.Pos{-7, 1, 0}, cube.Pos{-7, 1, 0}, block.Glowstone{})
	})
	// A cow: the fox's own sensors would swap find_cover for its JSON one.
	f := w.Spawn("minecraft:cow", mgl64.Vec3{0.5, 1, 0.5})
	install(t, w, f, "minecraft:behavior.find_cover", `{}`)
	until(t, w, f, 400, "the fox under the roof", func(m *mob.Mob) bool {
		p := m.Position()
		return p[0] < -3.5 && p[0] > -9 && math.Abs(p[2]) < 3.5
	})
}

func TestEatCarriedItem(t *testing.T) {
	w, f := arena(t, mobtest.Options{}, "minecraft:fox", "minecraft:behavior.eat_carried_item", `{"delay_before_eating":0.5}`)
	f.With(func(m *mob.Mob) { m.SetHeldItems(item.NewStack(item.Apple{}, 1), item.Stack{}) })
	w.Tick(5)
	if main, _ := heldOf(w, f); main.Empty() {
		t.Fatal("ate before delay_before_eating")
	}
	until(t, w, f, 20, "the apple eaten", func(m *mob.Mob) bool { main, _ := m.HeldItems(); return main.Empty() })
}

func heldOf(w *mobtest.World, r *mobtest.Ref) (item.Stack, item.Stack) {
	var a, b item.Stack
	r.With(func(m *mob.Mob) { a, b = m.HeldItems() })
	return a, b
}

func TestRandomLookAroundAndSit(t *testing.T) {
	w, c := arena(t, mobtest.Options{}, "minecraft:fox", "minecraft:behavior.random_look_around_and_sit",
		`{"probability":1,"min_look_count":1,"max_look_count":1,"min_look_time":10,"max_look_time":10,"random_look_around_cooldown":100}`)
	w.Tick(5)
	until(t, w, c, 20, "sitting", func(m *mob.Mob) bool { return m.Sitting() })
	until(t, w, c, 20, "standing after one look", func(m *mob.Mob) bool { return !m.Sitting() })
}

func TestRaidGarden(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Arena(tx, 12, 3)
		tx.SetBlock(cube.Pos{5, 0, 0}, block.Farmland{Hydration: 7}, nil)
		tx.SetBlock(cube.Pos{5, 1, 0}, block.Carrot{}, nil)
		tx.SetBlock(cube.Pos{5, 0, 2}, block.Farmland{Hydration: 7}, nil)
		c := block.Carrot{}
		c.Growth = 7
		tx.SetBlock(cube.Pos{5, 1, 2}, c, nil)
	})
	r := w.Spawn("minecraft:rabbit", mgl64.Vec3{0.5, 1, 0.5})
	install(t, w, r, "minecraft:behavior.raid_garden", `{"blocks":["minecraft:carrots"],"search_range":8,"goal_radius":1}`)
	growth := func() (young, ripe int) {
		w.Do(func(tx *world.Tx) {
			young = tx.Block(cube.Pos{5, 1, 0}).(block.Carrot).Growth
			if c, ok := tx.Block(cube.Pos{5, 1, 2}).(block.Carrot); ok {
				ripe = c.Growth
			}
		})
		return
	}
	if _, ok := w.TickUntil(400, func(tx *world.Tx) bool { _, r := growth(); return r < 7 }); !ok {
		t.Fatalf("the rabbit never ate the ripe carrot:\n%v", w.Dump(r))
	}
	if y, r := growth(); y != 0 || r != 6 {
		t.Errorf("growth young %d ripe %d, want 0 and 6", y, r)
	}
}

func TestDefendTrustedTargetNeedsTrust(t *testing.T) {
	// Nothing is trusted without minecraft:trust (not modelled): the goal
	// never picks a target.
	w, f := arena(t, mobtest.Options{}, "minecraft:fox", "minecraft:behavior.defend_trusted_target", `{"within_radius":25}`)
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{5.5, 1, 0.5})
	p := w.AddPlayer("owner", mgl64.Vec3{3.5, 1, 0.5})
	_ = p
	z.With(func(m *mob.Mob) { m.Goals().ClearAll() })
	w.Tick(40)
	f.With(func(m *mob.Mob) {
		if _, ok := m.Target(); ok {
			t.Error("fox picked a target without trusting anyone")
		}
	})
}

func TestCroak(t *testing.T) {
	w, fr := arena(t, mobtest.Options{}, "minecraft:frog", "minecraft:behavior.croak", `{"interval":0.1,"duration":0.5}`)
	until(t, w, fr, 20, "croaking", func(m *mob.Mob) bool { return m.ActorFlag(protocol.EntityDataFlagCroaking) })
	until(t, w, fr, 20, "croak end", func(m *mob.Mob) bool { return !m.ActorFlag(protocol.EntityDataFlagCroaking) })
}

func TestEatMob(t *testing.T) {
	w, fr := arena(t, mobtest.Options{}, "minecraft:frog", "minecraft:behavior.eat_mob", `{"run_speed":2,"eat_animation_time":0.3,"reach_mob_distance":1.75,"loot_table":"loot_tables/entities/frog.json"}`)
	prey := w.Spawn("minecraft:chicken", mgl64.Vec3{4.5, 1, 0.5})
	prey.With(func(m *mob.Mob) { m.Goals().ClearAll() })
	w.Do(func(tx *world.Tx) {
		fm, _ := fr.Mob(tx)
		pm, _ := prey.Mob(tx)
		fm.SetTarget(pm)
	})
	if _, ok := w.TickUntil(200, func(tx *world.Tx) bool { _, ok := prey.Entity(tx); return !ok }); !ok {
		t.Fatalf("frog did not eat its target:\n%v", w.Dump(fr))
	}
}

func TestJumpToBlock(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 12, block.Stone{}) })
	fr := w.Spawn("minecraft:frog", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(3) // let the spawn groups settle, they re-add the JSON goal
	install(t, w, fr, "minecraft:behavior.jump_to_block",
		`{"cooldown_range":[5,5],"search_width":6,"search_height":2,"minimum_distance":2,"minimum_path_length":0,"max_velocity":1.5}`)
	start := fr.Pos()
	until(t, w, fr, 140, "jump_goal flag (airborne, after the 5 s cooldown)", func(m *mob.Mob) bool { return m.ActorFlag(protocol.EntityDataFlagJumpGoal) })
	until(t, w, fr, 100, "landing", func(m *mob.Mob) bool { return !m.ActorFlag(protocol.EntityDataFlagJumpGoal) })
	if d := fr.Pos().Sub(start); math.Hypot(d[0], d[2]) < 1.5 {
		t.Errorf("frog jumped only %.2f blocks", math.Hypot(d[0], d[2]))
	}
}

func TestLayEgg(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Arena(tx, 10, 3)
		w.Fill(tx, cube.Pos{4, -1, -1}, cube.Pos{6, -1, 1}, block.Stone{}) // sand falls
		w.Fill(tx, cube.Pos{4, 0, -1}, cube.Pos{6, 0, 1}, block.Sand{})
	})
	tu := w.Spawn("minecraft:turtle", mgl64.Vec3{0.5, 1, 0.5})
	install(t, w, tu, "minecraft:behavior.lay_egg", `{"search_range":8,"search_height":2,"goal_radius":1.5,"lay_seconds":0.5,"on_lay":{"event":"minecraft:laid_egg","target":"self"}}`)
	until(t, w, tu, 300, "laying_egg flag on the sand", func(m *mob.Mob) bool { return m.ActorFlag(protocol.EntityDataFlagLayingEgg) })
	if p := tu.Pos(); p[0] < 2.5 {
		t.Errorf("turtle lays at x %.1f, away from the sand", p[0])
	}
	until(t, w, tu, 40, "laying done", func(m *mob.Mob) bool { return !m.ActorFlag(protocol.EntityDataFlagLayingEgg) })
}

func TestRandomSearchAndDig(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 14, block.Dirt{})
	})
	s := w.Spawn("minecraft:sniffer", mgl64.Vec3{0.5, 1, 0.5})
	install(t, w, s, "minecraft:behavior.random_search_and_dig",
		`{"target_blocks":["minecraft:dirt"],"search_range_xz":5,"search_range_y":2,"goal_radius":2,"digging_duration_range":[1,1],"spawn_item_after_seconds":0.5,"spawn_item_pos_offset":1,"item_table":"loot_tables/gameplay/entities/sniffer_seeds.json"}`)
	until(t, w, s, 300, "digging", func(m *mob.Mob) bool { return m.ActorFlag(protocol.EntityDataFlagDigging) })
	until(t, w, s, 40, "dig end", func(m *mob.Mob) bool { return !m.ActorFlag(protocol.EntityDataFlagDigging) })
	// sniffer_seeds.json drops torchflower seeds or a pitcher pod, which
	// Dragonfly does not have: no item entity to check.
}

func TestSilverfishMergeWithStone(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Fill(tx, cube.Pos{-2, 0, -2}, cube.Pos{2, 3, 2}, block.Cobblestone{})
		w.Clear(tx, cube.Pos{0, 1, 0}, cube.Pos{0, 1, 0})
	})
	s := w.Spawn("minecraft:silverfish", mgl64.Vec3{0.5, 1, 0.5})
	install(t, w, s, "minecraft:behavior.silverfish_merge_with_stone", `{}`)
	if _, ok := w.TickUntil(400, func(tx *world.Tx) bool { _, ok := s.Entity(tx); return !ok }); !ok {
		t.Fatalf("silverfish never merged:\n%v", w.Dump(s))
	}
	n := 0
	w.Do(func(tx *world.Tx) {
		for p := range cube.Range3D(cube.Pos{-1, 0, -1}, cube.Pos{1, 2, 1}) {
			if _, ok := tx.Block(p).(block.InfestedCobblestone); ok {
				n++
			}
		}
	})
	if n != 1 {
		t.Errorf("%d infested cobblestone blocks, want 1", n)
	}
}

func TestSilverfishWakeUpFriends(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Arena(tx, 12, 0)
		for x := 3; x <= 5; x++ {
			tx.SetBlock(cube.Pos{x, 1, 3}, block.InfestedStone{}, nil)
		}
	})
	s := w.Spawn("minecraft:silverfish", mgl64.Vec3{0.5, 1, 0.5})
	install(t, w, s, "minecraft:behavior.silverfish_wake_up_friends", `{}`)
	p := w.AddPlayer("attacker", mgl64.Vec3{-3.5, 1, 0.5})
	w.Tick(5)
	if n := len(w.Mobs("minecraft:silverfish")); n != 1 {
		t.Fatalf("%d silverfish before the hit", n)
	}
	p.With(func(pl *player.Player) {
		m, _ := s.Mob(pl.Tx())
		m.Hurt(1, entity.AttackDamageSource{Attacker: pl})
	})
	count := func(tx *world.Tx) int {
		n := 0
		for e := range tx.Entities() {
			if m, ok := mob.Of(e); ok && m.Identifier() == "minecraft:silverfish" {
				n++
			}
		}
		return n
	}
	if _, ok := w.TickUntil(40, func(tx *world.Tx) bool { return count(tx) > 1 }); !ok {
		t.Fatal("no silverfish came out of the infested stone")
	}
}
