//go:build crossfly_full

package misc_test

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/goal/misc"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	_ "github.com/df-mc/dragonfly/server/mob/vanilla"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

const (
	move = mob.FlagMove
	look = mob.FlagLook
	jump = mob.FlagJump
)

func groups(m *mob.Mob) []string {
	if g, ok := m.Instance().(mob.InstanceGroups); ok {
		return g.ActiveGroups()
	}
	return nil
}

func hasGroup(r *mobtest.Ref, g string) bool {
	ok := false
	r.With(func(m *mob.Mob) { ok = slices.Contains(groups(m), g) })
	return ok
}

func arena(t *testing.T, r int) *mobtest.World {
	mobtest.RequireDefinitions(t)
	mobtest.RequireFilters(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Arena(tx, r, 3) })
	return w
}

func goal(t *testing.T, key, raw string) mob.Goal {
	t.Helper()
	g, err := misc.Goal(key, raw)
	if err != nil {
		t.Fatalf("%v: %v", key, err)
	}
	return g
}

func eq(t *testing.T, what string, got, want map[string]any) {
	t.Helper()
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%v %v = %v (%T), want %v (%T)", what, k, got[k], got[k], v, v)
		}
	}
}

// swap replaces a mob's goals with g alone (after the spawn groups have
// settled), so a test sees one goal's behaviour.
func swap(w *mobtest.World, r *mobtest.Ref, key string, g mob.Goal, clear bool) {
	w.Tick(3)
	r.With(func(m *mob.Mob) {
		if clear {
			for _, k := range m.Goals().Keys() {
				m.Goals().Remove(k)
			}
		}
		m.Goals().Add("test/"+key, 0, g)
	})
}

func setTarget(w *mobtest.World, r *mobtest.Ref, target interface {
	Entity(tx *world.Tx) (world.Entity, bool)
}) {
	w.Do(func(tx *world.Tx) {
		m, _ := r.Mob(tx)
		e, _ := target.Entity(tx)
		m.SetTarget(e)
	})
}

func flagSeen(w *mobtest.World, r *mobtest.Ref, n int, flag uint8) (int, bool) {
	return w.TickUntil(n, func(tx *world.Tx) bool {
		m, ok := r.Mob(tx)
		return ok && m.ActorFlag(flag)
	})
}

// TestBDSDefaults checks every goal's defaults (BDS definitions and
// constructors, see the goal doc comments) and the vanilla values.
func TestBDSDefaults(t *testing.T) {
	eq(t, "timer_flag", misc.Params(goal(t, "minecraft:behavior.timer_flag_1", `{}`)), map[string]any{"cdMin": 10.0, "cdMax": 10.0, "durMin": 2.0, "flags": mob.ControlFlags(0)})
	eq(t, "ram_attack", misc.Params(goal(t, "minecraft:behavior.ram_attack", `{}`)), map[string]any{"run": 1.0, "ram": 2.0, "min": 4.0, "dist": 7.0, "force": 5.0, "height": 0.1, "baby": 1.0 / 3, "cdMin": 10.0, "cdMax": 20.0, "flags": move | look})
	eq(t, "charge_attack", misc.Params(goal(t, "minecraft:behavior.charge_attack", `{}`)), map[string]any{"min": 2.0, "rate": 0.1428, "speed": 1.0, "flags": move})
	eq(t, "knockback_roar", misc.Params(goal(t, "minecraft:behavior.knockback_roar", `{}`)), map[string]any{"duration": 20, "attack": 10, "damage": 6.0, "h": 4.0, "v": 4.0, "range": 4.0, "cooldown": 2, "flags": move | look | jump})
	eq(t, "move_to_block", misc.Params(goal(t, "minecraft:behavior.move_to_block", `{}`)), map[string]any{"interval": 20, "chance": 1.0, "range": 0, "height": 1, "radius": 0.5, "stay": 0, "speed": 1.0, "flags": move})
	eq(t, "stomp_turtle_egg", misc.Params(goal(t, "minecraft:behavior.stomp_turtle_egg", `{}`)), map[string]any{"interval": 20, "range": 10, "height": 1, "radius": 0.5, "blocks": 1})
	eq(t, "avoid_block", misc.Params(goal(t, "minecraft:behavior.avoid_block", `{}`)), map[string]any{"interval": 1, "range": 0, "height": 0, "walk": 1.0, "sprint": 1.0, "sMin": 3.0, "sMax": 8.0, "flags": move})
	eq(t, "move_to_random_block", misc.Params(goal(t, "minecraft:behavior.move_to_random_block", `{}`)), map[string]any{"distance": 16.0, "radius": 0.0, "speed": 1.0})
	eq(t, "move_towards_target", misc.Params(goal(t, "minecraft:behavior.move_towards_target", `{}`)), map[string]any{"radius": 0.0, "speed": 1.0, "flags": move})
	eq(t, "target_when_pushed", misc.Params(goal(t, "minecraft:behavior.target_when_pushed", `{}`)), map[string]any{"chance": 5.0, "filters": 0})
	eq(t, "hold_ground", misc.Params(goal(t, "minecraft:behavior.hold_ground", `{}`)), map[string]any{"radius": 10.0, "broadcast": false, "bcRange": 0.0, "flags": move | look})
	eq(t, "follow_mob", misc.Params(goal(t, "minecraft:behavior.follow_mob", `{}`)), map[string]any{"range": 0.0, "stop": 2.0, "speed": 1.0, "flags": move | look})
	eq(t, "look_at_target", misc.Params(goal(t, "minecraft:behavior.look_at_target", `{}`)), map[string]any{"min": 2.0, "max": 4.0, "flags": look})
	eq(t, "send_event", misc.Params(goal(t, "minecraft:behavior.send_event", `{"event_choices":[{"sequence":[{"event":"x"}]}]}`)), map[string]any{"maxR": 16.0, "weight": 1, "cast": 0, "steps": 1})
	eq(t, "summon_entity", misc.Params(goal(t, "minecraft:behavior.summon_entity", `{"summon_choices":[{"sequence":[{"entity_type":"minecraft:vex"}]}]}`)), map[string]any{"maxR": 32.0, "count": 1, "size": 1.0, "casting": true})

	// Vanilla values.
	eq(t, "goat ram", misc.Params(goal(t, "minecraft:behavior.ram_attack", string(mobtest.Component(t, "goat.json", "ram_default", "minecraft:behavior.ram_attack")))),
		map[string]any{"run": 0.7, "ram": 1.8, "min": 4.0, "dist": 7.0, "force": 2.5, "height": 0.04, "cdMin": 30.0, "cdMax": 300.0})
	eq(t, "ravager roar", misc.Params(goal(t, "minecraft:behavior.knockback_roar", string(mobtest.Component(t, "ravager.json", "roaring", "minecraft:behavior.knockback_roar")))),
		map[string]any{"duration": 20, "attack": 10, "damage": 6.0, "h": 3.0, "v": 3.0, "range": 4.0})
	eq(t, "zombie stomp_turtle_egg", misc.Params(goal(t, "minecraft:behavior.stomp_turtle_egg", string(mobtest.Component(t, "zombie.json", "", "minecraft:behavior.stomp_turtle_egg")))),
		map[string]any{"interval": 20, "range": 10, "height": 2, "radius": 1.14})
	eq(t, "hoglin avoid_block", misc.Params(goal(t, "minecraft:behavior.avoid_block", string(mobtest.Component(t, "hoglin.json", "", "minecraft:behavior.avoid_block")))),
		map[string]any{"interval": 5, "range": 8, "height": 4, "sMin": 2.0, "sMax": 5.0})
	eq(t, "pillager hold_ground", misc.Params(goal(t, "minecraft:behavior.hold_ground", string(mobtest.Component(t, "pillager.json", "minecraft:patrol_captain", "minecraft:behavior.hold_ground")))),
		map[string]any{"radius": 10.0, "broadcast": true, "bcRange": 8.0})
	eq(t, "iron golem move_towards_target", misc.Params(goal(t, "minecraft:behavior.move_towards_target", string(mobtest.Component(t, "iron_golem.json", "", "minecraft:behavior.move_towards_target")))),
		map[string]any{"radius": 32.0, "speed": 0.9})
	eq(t, "sniffer timer_flag_1", misc.Params(goal(t, "minecraft:behavior.timer_flag_1", string(mobtest.Component(t, "sniffer.json", "", "minecraft:behavior.timer_flag_1")))),
		map[string]any{"cdMin": 400.0, "cdMax": 500.0, "durMin": 2.0, "flags": move | look})
}

// TestTimerFlag: timer_flag_1 starts a cooldown roll after it is added, shows
// timer_flag_1 for duration_range, fires on_start and on_end, then waits
// cooldown_range before it runs again.
func TestTimerFlag(t *testing.T) {
	w := arena(t, 8)
	c := w.Spawn("minecraft:cow", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:entity_born"})
	swap(w, c, "timer", goal(t, "minecraft:behavior.timer_flag_1", `{"cooldown_range": 1, "duration_range": 2, "on_end": {"event": "minecraft:ageable_grow_up"}}`), true)
	n, ok := flagSeen(w, c, 60, protocol.EntityDataFlagTimerFlag1)
	if !ok {
		t.Fatalf("timer_flag_1 never set: %v", w.Dump(c))
	}
	if n < 19 || n > 23 {
		t.Errorf("timer started after %d ticks, want the 20-tick cooldown", n)
	}
	m, ok := w.TickUntil(60, func(tx *world.Tx) bool {
		cm, _ := c.Mob(tx)
		return !cm.ActorFlag(protocol.EntityDataFlagTimerFlag1)
	})
	if !ok || m < 39 || m > 42 {
		t.Errorf("flag lasted %d ticks (%v), want 40", m, ok)
	}
	c.With(func(cm *mob.Mob) {
		if cm.Baby() {
			t.Error("on_end (grow up) did not fire")
		}
	})
	if k, ok := flagSeen(w, c, 40, protocol.EntityDataFlagTimerFlag1); !ok || k < 19 {
		t.Errorf("next run after %d ticks (%v), want the 20-tick cooldown", k, ok)
	}
}

// TestGoatRams: a goat with a target 6 blocks away lowers its head, charges
// and knocks the target away, hurting it.
func TestGoatRams(t *testing.T) {
	w := arena(t, 14)
	p := w.AddPlayer("p", mgl64.Vec3{0.5, 1, 6.5})
	p.SetMaxHealth(200)
	g := w.Spawn("minecraft:goat", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:entity_spawned"})
	raw := string(mobtest.Component(t, "goat.json", "ram_default", "minecraft:behavior.ram_attack"))
	raw = raw[:len(raw)-1] + `, "cooldown_range": [0, 0]}`
	swap(w, g, "ram", goal(t, "minecraft:behavior.ram_attack", raw), true)
	setTarget(w, g, p)
	if _, ok := flagSeen(w, g, 80, protocol.EntityDataFlagRamAttack); !ok {
		t.Fatalf("goat never lowered its head: %v", w.Dump(g))
	}
	start := p.Pos()
	if _, ok := w.TickUntil(120, func(tx *world.Tx) bool { return p.HealthTx(tx) < 200 }); !ok {
		t.Fatalf("goat never hit: %v", w.Dump(g))
	}
	w.Tick(6)
	if d := p.Pos().Sub(start); math.Hypot(d[0], d[2]) < 0.5 || d[2] < 0 {
		t.Errorf("player not knocked away from the goat: moved %v", d)
	}
}

// TestVexCharges: a vex with a target charges (charging flag) and hits.
func TestVexCharges(t *testing.T) {
	w := arena(t, 10)
	p := w.AddPlayer("p", mgl64.Vec3{0.5, 1, 6.5})
	p.SetMaxHealth(200)
	v := w.Spawn("minecraft:vex", mgl64.Vec3{0.5, 2, 0.5})
	swap(w, v, "charge", goal(t, "minecraft:behavior.charge_attack", `{"success_rate": 1}`), true)
	setTarget(w, v, p)
	if _, ok := flagSeen(w, v, 40, protocol.EntityDataFlagCharging); !ok {
		t.Fatalf("vex never charged: %v", w.Dump(v))
	}
	if _, ok := w.TickUntil(200, func(tx *world.Tx) bool { return p.HealthTx(tx) < 200 }); !ok {
		t.Errorf("vex charge never hit: %v", w.Dump(v))
	}
}

// TestRavagerRoar: start_roar makes the ravager roar; half a second in,
// a zombie next to it is hurt for 6 and pushed, a pillager (illager) only
// pushed; on_roar_end brings back the hostile group.
func TestRavagerRoar(t *testing.T) {
	w := arena(t, 12)
	r := w.Spawn("minecraft:ravager", mgl64.Vec3{0.5, 1, 0.5})
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{3, 1, 0.5})
	pl := w.Spawn("minecraft:pillager", mgl64.Vec3{-2.5, 1, 0.5})
	w.Tick(2)
	var zh, ph float64
	z.With(func(m *mob.Mob) { zh = m.Health() })
	pl.With(func(m *mob.Mob) { ph = m.Health() })
	r.With(func(m *mob.Mob) { m.Fire("minecraft:start_roar", nil) })
	if _, ok := flagSeen(w, r, 3, protocol.EntityDataFlagRoaring); !ok {
		t.Fatalf("no roaring flag: %v", w.Dump(r))
	}
	w.Tick(12)
	z.With(func(m *mob.Mob) {
		if zh-m.Health() != 6 {
			t.Errorf("zombie lost %v health, want 6", zh-m.Health())
		}
		if m.Position()[0] < 3.2 {
			t.Errorf("zombie not pushed away: x %.2f", m.Position()[0])
		}
	})
	pl.With(func(m *mob.Mob) {
		if m.Health() != ph {
			t.Errorf("pillager hurt by the roar")
		}
	})
	if _, ok := w.TickUntil(30, func(*world.Tx) bool { return hasGroup(r, "minecraft:hostile") }); !ok {
		t.Errorf("on_roar_end never fired: %v", w.Dump(r))
	}
}

// TestMoveToBlock: a cow with move_to_block walks to the gold block and
// fires on_reach.
func TestMoveToBlock(t *testing.T) {
	w := arena(t, 12)
	w.Do(func(tx *world.Tx) { tx.SetBlock(cube.Pos{6, 0, 6}, block.Gold{}, nil) })
	c := w.Spawn("minecraft:cow", mgl64.Vec3{0.5, 1, 0.5})
	swap(w, c, "move_to_block", goal(t, "minecraft:behavior.move_to_block", `{"search_range": 8, "search_height": 2, "tick_interval": 1, "goal_radius": 1, "target_offset": [0, 1, 0], "target_blocks": ["minecraft:gold_block"], "stay_duration": 1, "on_reach": [{"event": "minecraft:ageable_grow_up"}]}`), true)
	if _, ok := w.TickUntil(300, func(tx *world.Tx) bool { return mobtest.HDistTx(tx, c, at(6.5, 6.5)) < 1.6 }); !ok {
		t.Fatalf("cow never reached the gold block: %v", w.Dump(c))
	}
}

// TestStompTurtleEggNeedsEggs: without turtle eggs (Dragonfly has none) the
// zombie's stomp_turtle_egg never runs.
func TestStompTurtleEggNeedsEggs(t *testing.T) {
	w := arena(t, 8)
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{0.5, 1, 0.5})
	w.Each(60, func(tx *world.Tx, _ int) bool {
		m, _ := z.Mob(tx)
		if m.Goals().Running("minecraft:behavior.stomp_turtle_egg") {
			t.Fatal("stomp_turtle_egg ran without eggs")
		}
		return true
	})
}

// TestAvoidBlock: a hoglin with the vanilla avoid_block parameters flees
// from a listed block (gold here: a portal block breaks without its frame).
func TestAvoidBlock(t *testing.T) {
	w := arena(t, 16)
	w.Do(func(tx *world.Tx) { tx.SetBlock(cube.Pos{2, 1, 0}, block.Gold{}, nil) })
	h := w.Spawn("minecraft:hoglin", mgl64.Vec3{0.5, 1, 0.5})
	raw := string(mobtest.Component(t, "hoglin.json", "", "minecraft:behavior.avoid_block"))
	raw = strings.Replace(raw, "minecraft:warped_fungus", "minecraft:gold_block", 1)
	swap(w, h, "avoid", goal(t, "minecraft:behavior.avoid_block", raw), true)
	if _, ok := w.TickUntil(200, func(tx *world.Tx) bool { return mobtest.HDistTx(tx, h, at(2.5, 0.5)) > 7 }); !ok {
		t.Errorf("hoglin did not flee from the block: %v", w.Dump(h))
	}
}

// TestMoveToRandomBlock: a mob without a target wanders off far.
func TestMoveToRandomBlock(t *testing.T) {
	w := arena(t, 24)
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{0.5, 1, 0.5})
	swap(w, z, "random_block", goal(t, "minecraft:behavior.move_to_random_block", `{"block_distance": 18, "speed_multiplier": 1}`), true)
	if _, ok := w.TickUntil(400, func(tx *world.Tx) bool { return mobtest.HDistTx(tx, z, at(0.5, 0.5)) > 6 }); !ok {
		t.Errorf("mob did not move far: %v", w.Dump(z))
	}
}

// TestMoveTowardsTarget: an iron golem walks towards a far target.
func TestMoveTowardsTarget(t *testing.T) {
	w := arena(t, 24)
	g := w.Spawn("minecraft:iron_golem", mgl64.Vec3{0.5, 1, 0.5})
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{0.5, 1, 20.5})
	swap(w, g, "towards", goal(t, "minecraft:behavior.move_towards_target", `{"within_radius": 32, "speed_multiplier": 0.9}`), true)
	z.With(func(m *mob.Mob) {
		for _, k := range m.Goals().Keys() {
			m.Goals().Remove(k)
		}
	})
	setTarget(w, g, z)
	if _, ok := w.TickUntil(300, func(tx *world.Tx) bool { return mobtest.DistTx(tx, g, z) < 10 }); !ok {
		t.Errorf("golem did not close in: %v", w.Dump(g))
	}
}

// TestTargetWhenPushed: an iron golem pushed by a matching mob targets it.
func TestTargetWhenPushed(t *testing.T) {
	w := arena(t, 8)
	g := w.Spawn("minecraft:iron_golem", mgl64.Vec3{0.5, 1, 0.5})
	pig := w.Spawn("minecraft:pig", mgl64.Vec3{0.9, 1, 0.5})
	swap(w, g, "pushed", goal(t, "minecraft:behavior.target_when_pushed", `{"percent_chance": 100, "entity_types": [{"filters": {"test": "is_family", "subject": "other", "value": "pig"}}]}`), true)
	if _, ok := w.TickUntil(20, func(tx *world.Tx) bool {
		m, _ := g.Mob(tx)
		t, _ := m.Target()
		return t != nil && t.H() == pig.H()
	}); !ok {
		t.Errorf("golem did not target the pushing pig: %v", w.Dump(g))
	}
}

// TestHoldGround: a pillager with a target within min_radius stands still,
// fires within_radius_event and passes the target to another pillager.
func TestHoldGround(t *testing.T) {
	w := arena(t, 12)
	p := w.AddPlayer("p", mgl64.Vec3{0.5, 1, 7.5})
	a := w.Spawn("minecraft:pillager", mgl64.Vec3{0.5, 1, 0.5})
	b := w.Spawn("minecraft:pillager", mgl64.Vec3{4.5, 1, 0.5})
	swap(w, a, "hold", goal(t, "minecraft:behavior.hold_ground", string(mobtest.Component(t, "pillager.json", "minecraft:patrol_captain", "minecraft:behavior.hold_ground"))), true)
	b.With(func(m *mob.Mob) {
		for _, k := range m.Goals().Keys() {
			m.Goals().Remove(k)
		}
	})
	setTarget(w, a, p)
	w.Tick(5)
	w.Do(func(tx *world.Tx) {
		bm, _ := b.Mob(tx)
		if t2, ok := bm.Target(); !ok || t2.H() != p.H() {
			t.Errorf("target not broadcast to the second pillager")
		}
		am, _ := a.Mob(tx)
		if d := mobtest.HDistTx(tx, a, at(0.5, 0.5)); d > 0.3 {
			t.Errorf("pillager moved %.2f blocks", d)
		}
		_ = am
	})
}

// TestFollowMob: a cow follows a pig until within stop_distance.
func TestFollowMob(t *testing.T) {
	w := arena(t, 12)
	c := w.Spawn("minecraft:cow", mgl64.Vec3{0.5, 1, 0.5})
	pig := w.Spawn("minecraft:pig", mgl64.Vec3{7.5, 1, 0.5})
	pig.With(func(m *mob.Mob) {
		for _, k := range m.Goals().Keys() {
			m.Goals().Remove(k)
		}
	})
	swap(w, c, "follow", goal(t, "minecraft:behavior.follow_mob", `{"search_range": 10, "stop_distance": 2, "filters": {"test": "is_family", "subject": "other", "value": "pig"}}`), true)
	if _, ok := w.TickUntil(200, func(tx *world.Tx) bool { return mobtest.DistTx(tx, c, pig) < 3 }); !ok {
		t.Errorf("cow did not follow the pig: %v", w.Dump(c))
	}
}

// TestLookAtTarget: the head turns to the target.
func TestLookAtTarget(t *testing.T) {
	w := arena(t, 8)
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{0.5, 1, 0.5})
	pig := w.Spawn("minecraft:pig", mgl64.Vec3{-5.5, 1, 0.5})
	swap(w, z, "look", goal(t, "minecraft:behavior.look_at_target", `{}`), true)
	setTarget(w, z, pig)
	z.With(func(m *mob.Mob) { m.StopMovement() })
	w.Tick(20)
	z.With(func(m *mob.Mob) {
		want := 90.0 // facing -x
		d := math.Mod(math.Abs(m.HeadYaw()-want), 360)
		if d > 20 && d < 340 {
			t.Errorf("head yaw %.1f, want about %.0f", m.HeadYaw(), want)
		}
	})
}

// TestEvokerWololo: an evoker turns a blue sheep red (send_event).
func TestEvokerWololo(t *testing.T) {
	w := arena(t, 12)
	s := w.Spawn("minecraft:sheep", mgl64.Vec3{5.5, 1, 0.5})
	s.With(func(m *mob.Mob) {
		for _, k := range m.Goals().Keys() {
			m.Goals().Remove(k)
		}
		m.UpdateMeta(func(meta *mob.Meta) { meta.Color = 11 })
	})
	e := w.Spawn("minecraft:evocation_illager", mgl64.Vec3{0.5, 1, 0.5})
	if _, ok := flagSeen(w, e, 100, protocol.EntityDataFlagCasting); !ok {
		t.Fatalf("evoker never cast: %v", w.Dump(e))
	}
	if _, ok := w.TickUntil(80, func(*world.Tx) bool { return hasGroup(s, "minecraft:sheep_red") }); !ok {
		t.Errorf("wololo never turned the sheep red: %v", w.Dump(s))
	}
}

// TestEvokerFangs: an evoker with a player 2 blocks away bites them with
// fangs (summon_entity, 6 damage).
func TestEvokerFangs(t *testing.T) {
	w := arena(t, 12)
	p := w.AddPlayer("p", mgl64.Vec3{2.5, 1, 0.5})
	p.SetMaxHealth(200)
	e := w.Spawn("minecraft:evocation_illager", mgl64.Vec3{0.5, 1, 0.5})
	raw := `{"summon_choices": [{"cast_duration": 2, "cooldown_time": 5, "max_activation_range": 3, "sequence": [{"base_delay": 1, "shape": "circle", "num_entities_spawned": 5, "entity_type": "minecraft:evocation_fang", "size": 1.5, "target": "self"}, {"base_delay": 0.15, "shape": "circle", "num_entities_spawned": 8, "entity_type": "minecraft:evocation_fang", "size": 2.5, "target": "self"}]}]}`
	swap(w, e, "summon", goal(t, "minecraft:behavior.summon_entity", raw), true)
	setTarget(w, e, p)
	if _, ok := w.TickUntil(80, func(tx *world.Tx) bool { return p.HealthTx(tx) <= 194 }); !ok {
		t.Errorf("fangs never bit (health %v): %v", p.Health(), w.Dump(e))
	}
}

// TestEvokerSummonsVexes: the vex spell summons 3 vexes owned by the evoker,
// at most summon_cap.
func TestEvokerSummonsVexes(t *testing.T) {
	w := arena(t, 12)
	p := w.AddPlayer("p", mgl64.Vec3{8.5, 1, 0.5})
	e := w.Spawn("minecraft:evocation_illager", mgl64.Vec3{0.5, 1, 0.5})
	raw := `{"summon_choices": [{"cast_duration": 1, "cooldown_time": 0, "sequence": [{"base_delay": 0.5, "entity_type": "minecraft:vex", "shape": "circle", "num_entities_spawned": 3, "summon_cap_radius": 16, "size": 1, "summon_cap": 4, "summon_event": "minecraft:add_damage_timer"}]}]}`
	swap(w, e, "summon", goal(t, "minecraft:behavior.summon_entity", raw), true)
	setTarget(w, e, p)
	w.Tick(30)
	vexes := w.Mobs("minecraft:vex")
	if len(vexes) != 3 {
		t.Fatalf("%d vexes, want 3", len(vexes))
	}
	vexes[0].With(func(m *mob.Mob) {
		if m.Owner() != e.H() {
			t.Error("vex not owned by the evoker")
		}
	})
	w.Tick(40)
	if n := len(w.Mobs("minecraft:vex")); n != 4 {
		t.Errorf("%d vexes after a second cast, want the cap of 4", n)
	}
}

// TestFoxPrioritizedTarget: a fox picks its higher priority prey even when
// lower priority prey is closer (nearest_prioritized_attackable_target).
func TestFoxPrioritizedTarget(t *testing.T) {
	w := arena(t, 14)
	f := w.Spawn("minecraft:fox", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(3)
	red := hasGroup(f, "minecraft:fox_red")
	if !red && !hasGroup(f, "minecraft:fox_arctic") {
		t.Skipf("fox has neither variant group: %v", w.Dump(f))
	}
	// Red foxes prefer rabbits (0) to cod (1), arctic foxes the opposite.
	nearID, farID := "minecraft:cod", "minecraft:rabbit"
	if !red {
		nearID, farID = farID, nearID
	}
	near := w.Spawn(nearID, mgl64.Vec3{3.5, 1, 0.5})
	far := w.Spawn(farID, mgl64.Vec3{8.5, 1, 0.5})
	for _, r := range []*mobtest.Ref{near, far} {
		r.With(func(m *mob.Mob) {
			for _, k := range m.Goals().Keys() {
				m.Goals().Remove(k)
			}
		})
	}
	if _, ok := w.TickUntil(100, func(tx *world.Tx) bool {
		m, _ := f.Mob(tx)
		t, ok := m.Target()
		return ok && t.H() == far.H()
	}); !ok {
		t.Errorf("fox did not pick the far, higher priority %v: %v", farID, w.Dump(f))
	}
}

// TestRavagerDelayedAttack: the ravager's hit lands about 7 ticks into its
// swing (delayed_attack flag).
func TestRavagerDelayedAttack(t *testing.T) {
	w := arena(t, 10)
	p := w.AddPlayer("p", mgl64.Vec3{2.8, 1, 0.5})
	p.SetMaxHealth(200)
	r := w.Spawn("minecraft:ravager", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(2)
	setTarget(w, r, p)
	n, ok := flagSeen(w, r, 60, protocol.EntityDataFlagDelayedAttack)
	if !ok {
		t.Fatalf("ravager never swung: %v", w.Dump(r))
	}
	if p.Health() < 200 {
		t.Fatalf("hit landed with the swing start (tick %d)", n)
	}
	m, ok := w.TickUntil(20, func(tx *world.Tx) bool { return p.HealthTx(tx) < 200 })
	if !ok {
		t.Fatalf("swing never hit: %v", w.Dump(r))
	}
	if m < 5 || m > 9 {
		t.Errorf("hit %d ticks into the swing, want 7", m)
	}
}

// TestPolarBearStomp: an angry polar bear rears up (standing flag) and hits.
func TestPolarBearStomp(t *testing.T) {
	w := arena(t, 10)
	p := w.AddPlayer("p", mgl64.Vec3{4.5, 1, 0.5})
	p.SetMaxHealth(200)
	b := w.Spawn("minecraft:polar_bear", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(2)
	if !hasGroup(b, "minecraft:adult_wild") {
		t.Skip("baby bear")
	}
	setTarget(w, b, p)
	b.With(func(m *mob.Mob) { m.Fire("minecraft:on_anger", nil) })
	if _, ok := flagSeen(w, b, 100, protocol.EntityDataFlagStanding); !ok {
		t.Errorf("bear never reared up: %v", w.Dump(b))
	}
	if _, ok := w.TickUntil(100, func(tx *world.Tx) bool { return p.HealthTx(tx) < 200 }); !ok {
		t.Errorf("bear never hit: %v", w.Dump(b))
	}
}

// TestCannotBeAttacked: mobs cannot hurt a ghast; players can.
func TestCannotBeAttacked(t *testing.T) {
	w := arena(t, 8)
	g := w.Spawn("minecraft:ghast", mgl64.Vec3{0.5, 3, 0.5})
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{4.5, 1, 0.5})
	p := w.AddPlayer("p", mgl64.Vec3{-4.5, 1, 0.5})
	w.Do(func(tx *world.Tx) {
		gm, _ := g.Mob(tx)
		zm, _ := z.Mob(tx)
		pe, _ := p.Entity(tx)
		if n, _ := gm.Hurt(2, entity.AttackDamageSource{Attacker: zm}); n != 0 {
			t.Error("zombie hurt the ghast")
		}
		if n, _ := gm.Hurt(2, entity.AttackDamageSource{Attacker: pe.(*player.Player)}); n == 0 {
			t.Error("player could not hurt the ghast")
		}
	})
}

// TestOnDeath: the ender dragon fires start_death when it dies.
func TestOnDeath(t *testing.T) {
	w := arena(t, 8)
	d := w.Spawn("minecraft:ender_dragon", mgl64.Vec3{0.5, 5, 0.5})
	w.Tick(2)
	var before []string
	d.With(func(m *mob.Mob) {
		before = groups(m)
		m.Kill(entity.AttackDamageSource{})
	})
	var after []string
	w.Do(func(tx *world.Tx) {
		if m, ok := d.Mob(tx); ok {
			after = groups(m)
		}
	})
	if slices.Equal(before, after) {
		t.Errorf("start_death changed no group: %v", after)
	}
}

func at(x, z float64) mobtest.Located { return fixed{mgl64.Vec3{x, 1, z}} }

type fixed struct{ p mgl64.Vec3 }

func (f fixed) PosTx(*world.Tx) mgl64.Vec3 { return f.p }
func (f fixed) Pos() mgl64.Vec3            { return f.p }
