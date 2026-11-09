//go:build crossfly_full

package combat

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	ccombat "github.com/df-mc/dragonfly/server/mob/component/combat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func defineMobs() {
	def("test:villager", `{"test:family": {"family": ["villager", "mob"]}}`, nil, nil)
	def("test:cow", `{"test:family": {"family": ["cow", "mob"]}}`, nil, nil)
	def("test:breeze", `{"test:family": {"family": ["breeze", "mob"]}}`, nil, nil)
	// Zombie: the vanilla 1.26 target/attack goals.
	def("test:zombie", `{
		"test:family": {"family": ["zombie", "monster", "mob"]},
		"minecraft:attack": {"damage": 3},
		"minecraft:behavior.hurt_by_target": {"entity_types": [{"filters": {"operator": "not", "test": "is_family", "subject": "other", "value": "breeze"}}], "alert_same_type": true, "priority": 1},
		"minecraft:behavior.nearest_attackable_target": {"reselect_targets": true, "must_see": true, "within_radius": 25, "must_see_forget_duration": 17,
			"entity_types": [
				{"filters": {"any_of": [{"test": "is_family", "subject": "other", "value": "player"}, {"test": "is_family", "subject": "other", "value": "snowgolem"}]}, "max_dist": 35},
				{"filters": {"any_of": [{"test": "is_family", "subject": "other", "value": "villager"}]}, "max_dist": 35, "must_see": false}
			], "priority": 2},
		"minecraft:behavior.melee_box_attack": {"can_spread_on_fire": true, "priority": 4},
		"minecraft:burns_in_daylight": {}
	}`, nil, nil)
	def("test:skeleton", `{
		"test:family": {"family": ["skeleton", "monster", "mob"]},
		"minecraft:behavior.ranged_attack": {"attack_interval": {"min": 3.0, "max": 3.0}, "attack_range": {"min": 0.0, "max": 15.0}, "priority": 0},
		"minecraft:shooter": {"def": "minecraft:arrow", "sound": "bow"},
		"minecraft:burns_in_daylight": {}
	}`, nil, nil)
	def("test:spider", `{
		"test:family": {"family": ["spider", "monster", "mob"]},
		"minecraft:behavior.leap_at_target": {"must_be_on_ground": false, "priority": 4, "yd": 0.4}
	}`, nil, nil)
	def("test:creeper", `{
		"test:family": {"family": ["creeper", "monster", "mob"]},
		"minecraft:behavior.swell": {"priority": 2, "start_distance": 2.5, "stop_distance": 6},
		"minecraft:on_target_escape": {"event": "minecraft:stop_exploding", "target": "self"},
		"minecraft:target_nearby_sensor": {"inside_range": 2.5, "on_inside_range": {"event": "minecraft:start_exploding", "target": "self"},
			"on_vision_lost_inside_range": {"event": "minecraft:stop_exploding", "target": "self"}, "must_see": true,
			"on_outside_range": {"event": "minecraft:stop_exploding", "target": "self"}, "outside_range": 6}
	}`, map[string]string{
		"minecraft:exploding": `{"minecraft:explode": {"causes_fire": false, "fuse_lit": true, "power": 3, "destroy_affected_by_griefing": true, "fuse_length": 1.5}}`,
	}, map[string]testEvent{
		"minecraft:start_exploding": {add: []string{"minecraft:exploding"}},
		"minecraft:stop_exploding":  {remove: []string{"minecraft:exploding"}},
	})
	def("test:sensors", `{
		"test:family": {"family": ["mob"]},
		"minecraft:environment_sensor": {"triggers": [{"event": "day", "filters": {"test": "is_daytime"}}, {"event": "night", "filters": {"test": "is_daytime", "value": false}}]},
		"minecraft:damage_sensor": {"triggers": [{"cause": "fall", "deals_damage": "no"}, {"cause": "entity_attack", "on_damage": {"event": "hit_by_villager", "filters": {"test": "is_family", "subject": "other", "value": "villager"}}, "damage_multiplier": 2}]},
		"minecraft:on_target_acquired": {"event": "got_target"},
		"minecraft:angry": {"duration": 2, "calm_event": {"event": "calmed", "target": "self"}}
	}`, nil, nil)
	def("test:convert", `{
		"test:family": {"family": ["zombie", "mob"]},
		"minecraft:transformation": {"into": "test:villager<test:converted>", "delay": {"value": 1}, "preserve_equipment": true}
	}`, nil, nil)
}

var origin = mgl64.Vec3{0.5, 10, 0.5}

func TestNearestAttackableTargetFilters(t *testing.T) {
	run(t, func(e *env) {
		z := e.spawn("test:zombie", origin)
		e.spawn("test:cow", origin.Add(mgl64.Vec3{3, 0, 0}))
		v := e.spawn("test:villager", origin.Add(mgl64.Vec3{10, 0, 0}))
		e.tick(12, []*mob.Mob{z}, func(int) bool { _, ok := z.Target(); return ok })
		if tg, ok := z.Target(); !ok || tg.H() != v.H() {
			fatalf(t, "zombie should target the villager (closest match, cow filtered out), got %v", tg)
		}
	})
	run(t, func(e *env) {
		z := e.spawn("test:zombie", origin)
		p := e.player(origin.Add(mgl64.Vec3{0, 0, 8}))
		// A wall between them: the player entry has no must_see of its own,
		// so the goal's must_see applies.
		for x := -2; x <= 2; x++ {
			for y := 10; y <= 13; y++ {
				e.tx.SetBlock(cube.Pos{x, y, 4}, block.Stone{}, nil)
			}
		}
		e.tick(25, []*mob.Mob{z}, nil)
		if _, ok := z.Target(); ok {
			fatal(t, "zombie targeted a player it cannot see")
		}
		for x := -2; x <= 2; x++ {
			for y := 10; y <= 13; y++ {
				e.tx.SetBlock(cube.Pos{x, y, 4}, nil, nil)
			}
		}
		e.tick(12, []*mob.Mob{z}, func(int) bool { _, ok := z.Target(); return ok })
		if tg, ok := z.Target(); !ok || tg.H() != p.H() {
			fatal(t, "zombie did not target the visible player")
		}
		// Creative players are never targets.
		p.SetGameMode(world.GameModeCreative)
		e.tick(2, []*mob.Mob{z}, nil)
		if _, ok := z.Target(); ok {
			fatal(t, "zombie kept a creative player as target")
		}
	})
}

func TestHurtByTargetAlerts(t *testing.T) {
	run(t, func(e *env) {
		a := e.spawn("test:zombie", origin)
		b := e.spawn("test:zombie", origin.Add(mgl64.Vec3{4, 0, 0}))
		v := e.spawn("test:villager", origin.Add(mgl64.Vec3{2, 0, 2}))
		// Keep the villager out of nearest_attackable_target: far zombies
		// would target it anyway, so check right after the hit.
		a.Hurt(1, entity.AttackDamageSource{Attacker: v})
		e.tick(1, []*mob.Mob{a}, nil)
		if tg, ok := a.Target(); !ok || tg.H() != v.H() {
			fatal(t, "hurt zombie did not target its attacker")
		}
		if tg, ok := b.Target(); !ok || tg.H() != v.H() {
			fatal(t, "alert_same_type did not alert the other zombie")
		}
	})
	run(t, func(e *env) {
		a := e.spawn("test:zombie", origin)
		br := e.spawn("test:breeze", origin.Add(mgl64.Vec3{2, 0, 0}))
		a.Hurt(1, entity.AttackDamageSource{Attacker: br})
		e.tick(1, []*mob.Mob{a}, nil)
		if _, ok := a.Target(); ok {
			fatal(t, "zombie targeted a breeze despite the entity_types filter")
		}
	})
}

func TestMeleeBoxReachAndCooldown(t *testing.T) {
	run(t, func(e *env) {
		z := e.spawn("test:zombie", origin)
		v := e.spawn("test:villager", origin.Add(mgl64.Vec3{0, 0, 1.2}))
		v.SetMaxHealth(100)
		v.SetHealth(100)
		z.SetTarget(v)
		var hits []int
		last := v.Health()
		e.tick(70, []*mob.Mob{z, v}, func(i int) bool {
			if h := v.Health(); h < last {
				hits = append(hits, i)
				last = h
			}
			return false
		})
		if len(hits) != 3 {
			fatalf(t, "want 3 hits in 70 ticks (cooldown 20), got %v", hits)
		}
		if d := hits[1] - hits[0]; d != 20 {
			fatalf(t, "hits %v: cooldown %d, want 20", hits, d)
		}
		if got := 100 - v.Health(); got != 9 {
			fatalf(t, "3 hits of 3 dealt %v", got)
		}
	})
	run(t, func(e *env) {
		z := e.spawn("test:zombie", origin)
		v := e.spawn("test:villager", origin.Add(mgl64.Vec3{0, 0, 2.5}))
		z.SetTarget(v)
		navOf(z).noPath = false
		g, _ := z.Goals().Goal("minecraft:behavior.melee_box_attack")
		box := g.(*meleeBoxAttack).AttackBox(z)
		if box.IntersectsWith(v.BBox()) {
			fatal(t, "box reaches 2.5 blocks")
		}
		if !box.IntersectsWith(v.BBox().Translate(mgl64.Vec3{0, 0, -1.2})) {
			fatal(t, "box does not reach 1.3 blocks")
		}
	})
}

func TestMeleeAttackReach(t *testing.T) {
	g, err := newMeleeAttack(mob.GoalSpec{Key: "minecraft:behavior.melee_attack"})
	if err != nil {
		t.Fatal(err)
	}
	run(t, func(e *env) {
		z := e.spawn("test:zombie", origin)
		v := e.spawn("test:villager", origin.Add(mgl64.Vec3{0, 0, 1}))
		// (0.6 x 2)^2 + 0.6 = 2.04 (BDS _getAttackReachSqr).
		if r := g.(*meleeAttack).reachSq(z, v); r < 2.03 || r > 2.05 {
			fatalf(t, "reach² %v, want 2.04", r)
		}
	})
}

func TestSkeletonKeepsDistanceAndFires(t *testing.T) {
	run(t, func(e *env) {
		s := e.spawn("test:skeleton", origin)
		p := e.player(origin.Add(mgl64.Vec3{0, 0, 10}))
		s.SetTarget(p)
		var shots []int
		var settled mgl64.Vec3
		arrows := 0
		e.tick(260, []*mob.Mob{s}, func(i int) bool {
			if i == 25 {
				settled = s.Pos()
			}
			n := 0
			for en := range e.tx.Entities() {
				if en.H().Type() == entity.ArrowType {
					n++
				}
			}
			if n > arrows {
				shots = append(shots, i)
				arrows = n
			}
			return false
		})
		if navOf(s).active {
			fatal(t, "skeleton in range kept walking")
		}
		// It walks until it has seen the target for 1 s, then stands.
		if s.Pos().Sub(settled).Len() > 0.01 {
			fatalf(t, "skeleton moved from %v to %v although the target is in range", settled, s.Pos())
		}
		if len(shots) < 3 {
			fatalf(t, "want at least 3 shots in 260 ticks, got %v", shots)
		}
		for i := 1; i < len(shots); i++ {
			if d := shots[i] - shots[i-1]; d < 60 || d > 63 {
				fatalf(t, "shots %v: interval %d, want ~61 (3 s)", shots, d)
			}
		}
	})
	run(t, func(e *env) {
		s := e.spawn("test:skeleton", origin)
		p := e.player(origin.Add(mgl64.Vec3{0, 0, 22}))
		s.SetTarget(p)
		e.tick(5, []*mob.Mob{s}, nil)
		if n := navOf(s); !n.active || n.lastMove != "entity" {
			fatal(t, "skeleton out of range did not approach")
		}
	})
}

func TestCreeperSwellsAndExplodes(t *testing.T) {
	run(t, func(e *env) {
		c := e.spawn("test:creeper", origin)
		p := e.player(origin.Add(mgl64.Vec3{0, 0, 2}))
		c.SetTarget(p)
		e.tick(1, []*mob.Mob{c}, nil)
		if !c.Ignited() {
			fatal(t, "creeper not ignited with the target 2 blocks away (inside_range 2.5)")
		}
		e.tick(2, []*mob.Mob{c}, nil)
		if c.Swell() != 1 {
			fatalf(t, "swell %d, want 1", c.Swell())
		}
		h := c.H()
		n := e.tick(40, []*mob.Mob{c}, func(int) bool { return h.Closed() })
		if !h.Closed() {
			fatal(t, "creeper did not explode")
		}
		if n < 25 || n > 31 {
			fatalf(t, "exploded after %d ticks, want the 1.5 s fuse", n)
		}
		if p.Health() >= 20 {
			fatal(t, "explosion did not hurt the player")
		}
	})
	run(t, func(e *env) {
		c := e.spawn("test:creeper", origin)
		p := e.player(origin.Add(mgl64.Vec3{0, 0, 2}))
		c.SetTarget(p)
		e.tick(10, []*mob.Mob{c}, nil)
		if !c.Ignited() {
			fatal(t, "not ignited")
		}
		p.Teleport(origin.Add(mgl64.Vec3{0, 0, 8}))
		e.tick(2, []*mob.Mob{c}, nil)
		if c.Ignited() {
			fatal(t, "fuse still lit after the target left outside_range 6")
		}
		if c.Swell() != -1 {
			fatalf(t, "swell %d after the target left, want -1", c.Swell())
		}
		e.tick(40, []*mob.Mob{c}, nil)
		if c.H().Closed() {
			fatal(t, "creeper exploded after the target left")
		}
	})
	run(t, func(e *env) {
		c := e.spawn("test:creeper", origin)
		p := e.player(origin.Add(mgl64.Vec3{0, 0, 3.2})) // box gap 2.6: the sensor measures AABB::distanceTo(AABB)
		c.SetTarget(p)
		e.tick(5, []*mob.Mob{c}, nil)
		if c.Ignited() {
			fatal(t, "ignited at a 2.6 block gap (inside_range 2.5)")
		}
	})
}

func TestSpiderLeaps(t *testing.T) {
	run(t, func(e *env) {
		s := e.spawn("test:spider", origin)
		v := e.spawn("test:villager", origin.Add(mgl64.Vec3{3, 0, 0}))
		s.SetTarget(v)
		leapt := false
		e.tick(60, []*mob.Mob{s}, func(int) bool {
			if s.Goals().Running("minecraft:behavior.leap_at_target") {
				leapt = true
				return true
			}
			return false
		})
		if !leapt {
			fatal(t, "spider never leapt (1 in 5 per tick)")
		}
		vel := s.Velocity()
		if vel[1] < 0.25 || vel[0] < 0.2 {
			fatalf(t, "leap velocity %v, want up 0.4 (less gravity) and 0.4 towards the target (less ground friction)", vel)
		}
	})
	run(t, func(e *env) {
		s := e.spawn("test:spider", origin)
		v := e.spawn("test:villager", origin.Add(mgl64.Vec3{1, 0, 0}))
		s.SetTarget(v)
		e.tick(60, []*mob.Mob{s}, nil)
		if s.Goals().Running("minecraft:behavior.leap_at_target") || s.Velocity()[1] > 0.1 {
			fatal(t, "spider leapt at a target closer than 2 blocks")
		}
	})
}

func burnRun(t *testing.T, day bool, setup func(e *env, z *mob.Mob)) (burnt bool, z *mob.Mob, ev *env) {
	run(t, func(e *env) {
		if day {
			e.w.SetTime(6000)
		}
		z = e.spawn("test:zombie", origin)
		if setup != nil {
			setup(e, z)
		}
		e.tick(400, []*mob.Mob{z}, func(int) bool { return z.OnFireDuration() > 0 })
		burnt = z.OnFireDuration() > 0
		ev = e
	})
	return
}

func TestBurnsInDaylight(t *testing.T) {
	if burnt, _, _ := burnRun(t, true, nil); !burnt {
		t.Fatal("zombie did not burn in daylight")
	}
	if burnt, _, _ := burnRun(t, false, nil); burnt {
		t.Fatal("zombie burnt at night")
	}
	var helmet item.Stack
	burnt, _, _ := burnRun(t, true, func(e *env, z *mob.Mob) {
		z.Armour().SetHelmet(item.NewStack(item.Helmet{Tier: item.ArmourTierLeather{}}, 1))
	})
	_ = helmet
	if burnt {
		t.Fatal("helmeted zombie burnt")
	}
	burnt, _, _ = burnRun(t, true, func(e *env, z *mob.Mob) {
		for x := -6; x <= 6; x++ {
			for zz := -6; zz <= 6; zz++ {
				e.tx.SetBlock(cube.Pos{x, 13, zz}, block.Stone{}, nil)
			}
		}
	})
	if burnt {
		t.Fatal("shaded zombie burnt")
	}
}

func TestHelmetTakesDamage(t *testing.T) {
	run(t, func(e *env) {
		e.w.SetTime(6000)
		z := e.spawn("test:zombie", origin)
		z.Armour().SetHelmet(item.NewStack(item.Helmet{Tier: item.ArmourTierLeather{}}, 1))
		full := z.Armour().Helmet().Durability()
		e.tick(600, []*mob.Mob{z}, nil)
		if z.Armour().Helmet().Durability() >= full {
			fatal(t, "helmet took no damage in the sun")
		}
	})
}

func TestSensors(t *testing.T) {
	run(t, func(e *env) {
		s := e.spawn("test:sensors", origin)
		e.tick(1, []*mob.Mob{s}, nil)
		if !contains(fired(s), "night") || contains(fired(s), "day") {
			fatalf(t, "environment_sensor at night fired %v", fired(s))
		}
		// damage_sensor: fall damage is cancelled, a villager's hit doubled.
		s.SetMaxHealth(20)
		s.SetHealth(20)
		if n, _ := s.Hurt(5, entity.FallDamageSource{}); n != 0 || s.Health() != 20 {
			fatal(t, "fall damage was not cancelled")
		}
		v := e.spawn("test:villager", origin.Add(mgl64.Vec3{1, 0, 0}))
		if n, _ := s.Hurt(2, entity.AttackDamageSource{Attacker: v}); n != 4 {
			fatalf(t, "damage_multiplier 2: dealt %v", n)
		}
		if !contains(fired(s), "hit_by_villager") {
			fatal(t, "on_damage event did not fire")
		}
		// on_target_acquired, then angry: calm after 2 s and drop the target.
		s.SetTarget(v)
		if !contains(fired(s), "got_target") {
			fatal(t, "on_target_acquired did not fire")
		}
		e.tick(1, []*mob.Mob{s}, nil)
		if !s.Angry() {
			fatal(t, "not angry with a target")
		}
		e.tick(45, []*mob.Mob{s}, nil)
		if !contains(fired(s), "calmed") {
			fatal(t, "calm_event did not fire after the anger duration")
		}
		if _, ok := s.Target(); ok || s.Angry() {
			fatal(t, "calm mob kept its target or anger")
		}
	})
}

func TestTransformation(t *testing.T) {
	run(t, func(e *env) {
		z := e.spawn("test:convert", origin)
		z.SetHeldItems(item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1), item.Stack{})
		h := z.H()
		e.tick(25, []*mob.Mob{z}, func(int) bool { return h.Closed() })
		if !h.Closed() {
			fatal(t, "mob did not transform after its 1 s delay")
		}
		var n *mob.Mob
		for en := range e.tx.Entities() {
			if m, ok := mob.Of(en); ok && m.Identifier() == "test:villager" {
				n = m
			}
		}
		if n == nil {
			fatal(t, "no transformed mob")
		}
		if !contains(fired(n), "test:converted") || !contains(fired(n), mob.EventTransformed) {
			fatalf(t, "transformed mob events %v", fired(n))
		}
		if main, _ := n.HeldItems(); main.Empty() {
			fatal(t, "preserve_equipment lost the held item")
		}
	})
}

func TestExplodeNBT(t *testing.T) {
	run(t, func(e *env) {
		c := e.spawn("test:creeper", origin)
		c.Fire("minecraft:start_exploding", nil)
		ex, ok := mob.ComponentOf[*ccombat.Explode](c)
		if !ok || !ex.Lit() {
			fatal(t, "explode not lit")
		}
		e.tick(5, []*mob.Mob{c}, nil)
		data := ex.EncodeNBT()
		if data["Fuse"].(int32) != int32(ex.FuseLeft()) {
			fatalf(t, "fuse NBT %v", data)
		}
	})
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
