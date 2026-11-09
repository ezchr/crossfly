//go:build crossfly_full

package misc_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// The vanilla mobs whose behaviour runs on timer_flag_1/2/3, end to end with
// their own definitions.

// The sniffer's seeds are Crossfly Full mob items.
func init() { item.RegisterMobItems() }

func prop(w *mobtest.World, r *mobtest.Ref, name string) any {
	var v any
	r.With(func(m *mob.Mob) { v, _ = m.Property(name) })
	return v
}

func waitProp(t *testing.T, w *mobtest.World, r *mobtest.Ref, max int, name string, want any) int {
	t.Helper()
	n, ok := w.TickUntil(max, func(tx *world.Tx) bool {
		m, ok := r.Mob(tx)
		if !ok {
			return false
		}
		v, _ := m.Property(name)
		return v == want
	})
	if !ok {
		t.Fatalf("%v never became %v (is %v) in %d ticks:\n%v", name, want, prop(w, r, name), max, w.Dump(r))
	}
	return n
}

func has(r *mobtest.Ref, key string) (ok bool) {
	r.With(func(m *mob.Mob) { ok = m.HasComponent(key) })
	return ok
}

// TestArmadilloRollsUp: an undead mob near an armadillo makes it roll up
// (rolled_up, timer_flag_1 cycling rolled_up/peeking); once the threat is
// gone it relaxes, unrolls after the timer_flag_1 run and is unrolled when
// its 4 s timer runs out.
func TestArmadilloRollsUp(t *testing.T) {
	w := arena(t, 12)
	a := w.Spawn("minecraft:armadillo", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_adult"})
	w.Tick(5)
	const state = "minecraft:armadillo_state"
	if v := prop(w, a, state); v != "unrolled" {
		t.Fatalf("armadillo starts %v", v)
	}
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{3.5, 1, 0.5})
	z.With(func(m *mob.Mob) { m.Goals().ClearAll() })
	waitProp(t, w, a, 40, state, "rolled_up")
	if !has(a, "minecraft:behavior.timer_flag_1") || has(a, "minecraft:behavior.random_stroll") {
		t.Fatalf("rolled up armadillo has the wrong goals: %v", w.Dump(a))
	}
	// cooldown 2.5 s, then 5-20 s rolled up, then it peeks.
	n := waitProp(t, w, a, 50+400+10, state, "rolled_up_peeking")
	if n < 140 {
		t.Errorf("peeked after %d ticks, want at least 50+100", n)
	}
	z.With(func(m *mob.Mob) { _ = m.Close() })
	waitProp(t, w, a, 20, state, "rolled_up_relaxing")
	// timer_flag_1 of rolled_up_without_threats: 2.5 s cooldown, then
	// start_unrolling.
	n = waitProp(t, w, a, 70, state, "rolled_up_unrolling")
	if n < 45 {
		t.Errorf("unrolling after %d ticks, want 50", n)
	}
	waitProp(t, w, a, 80, state, "unrolled")
	if !has(a, "minecraft:behavior.random_stroll") {
		t.Errorf("unrolled armadillo cannot stroll: %v", w.Dump(a))
	}
}

// TestBeeWitherRose: a bee fed a wither rose gets wither from
// add_wither_effect, whose timer_flag_1 (cooldown 0, 0.05 s) starts on the
// next tick. Its on_end names "minecraft:on_wither_effect_added" while the
// event is "on_wither_effect_added"; BDS looks events up by exact name
// (ActorDefinitionDescriptor::_executeEvent), so the group stays and the
// timer keeps cycling, as in BDS. Feeding again re-adds the group, which
// rebuilds spell_effects.
func TestBeeWitherRose(t *testing.T) {
	w := arena(t, 8)
	b := w.Spawn("minecraft:bee", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_adult"})
	p := w.AddPlayer("p", mgl64.Vec3{1.5, 1, 0.5})
	w.Tick(3)
	if _, ok := p.Interact(b, item.NewStack(block.Flower{Type: block.WitherRose()}, 4)); !ok {
		t.Fatal("bee did not take the wither rose")
	}
	b.With(func(m *mob.Mob) {
		if _, ok := m.Effect(effect.Wither); !ok {
			t.Errorf("no wither after the rose: %v", m.Effects())
		}
	})
	n, ok := flagSeen(w, b, 10, protocol.EntityDataFlagTimerFlag1)
	if !ok || n > 3 {
		t.Errorf("timer_flag_1 after %d ticks (%v), want the next tick", n, ok)
	}
	if k, ok := w.TickUntil(10, func(tx *world.Tx) bool {
		m, _ := b.Mob(tx)
		return m != nil && !m.ActorFlag(protocol.EntityDataFlagTimerFlag1)
	}); !ok || k > 3 {
		t.Errorf("timer_flag_1 lasted %d ticks (%v), want 2", k, ok)
	}
}

// TestCreakingCrumbles: a twitching creaking runs timer_flag_1 (default 10 s
// cooldown, then 2.25 s) and crumbles at its end: crumbling, then gone
// (instant_despawn). start_twitching comes from a breaking creaking heart,
// which Dragonfly does not have; the test fires it. A heart-bound creaking
// without a heart crumbles at once (is_bound_to_creaking_heart), so this one
// is player spawned.
func TestCreakingCrumbles(t *testing.T) {
	w := arena(t, 8)
	c := w.Spawn("minecraft:creaking", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:entity_spawned"})
	w.Tick(3)
	c.With(func(m *mob.Mob) { m.Fire("minecraft:start_twitching", nil) })
	if v := prop(w, c, "minecraft:creaking_state"); v != "twitching" {
		t.Fatalf("creaking state %v after start_twitching", v)
	}
	n, ok := flagSeen(w, c, 220, protocol.EntityDataFlagTimerFlag1)
	if !ok || n < 195 {
		t.Fatalf("twitch flag after %d ticks (%v), want 200", n, ok)
	}
	if _, ok := w.TickUntil(60, func(*world.Tx) bool { return c.Gone() }); !ok {
		t.Fatalf("creaking never crumbled: %v", w.Dump(c))
	}
}

// TestSnifferSniffsAndDigs: an adult sniffer sniffs (timer_flag_1) after its
// 400-500 s cooldown, then searches dirt, digs (digging, not pushable), drops
// seeds, feels happy (timer_flag_3) and stands up (timer_flag_2).
func TestSnifferSniffsAndDigs(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 24, block.Dirt{}) })
	s := w.Spawn("minecraft:sniffer", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_adult"})
	n, ok := flagSeen(w, s, 10100, protocol.EntityDataFlagTimerFlag1)
	if !ok || n < 7990 {
		t.Fatalf("sniffing after %d ticks (%v), want 8000-10000", n, ok)
	}
	if _, ok := w.TickUntil(50, func(tx *world.Tx) bool {
		m, _ := s.Mob(tx)
		return m != nil && m.HasComponent("minecraft:behavior.random_search_and_dig")
	}); !ok {
		t.Fatalf("no search after sniffing: %v", w.Dump(s))
	}
	if _, ok := flagSeen(w, s, 600, protocol.EntityDataFlagDigging); !ok {
		t.Fatalf("never digs: %v", w.Dump(s))
	}
	if has(s, "minecraft:pushable_by_entity") {
		t.Error("digging sniffer is still pushable")
	}
	if _, ok := flagSeen(w, s, 200, protocol.EntityDataFlagTimerFlag3); !ok {
		t.Fatalf("never happy (item found): %v", w.Dump(s))
	}
	seeds := w.Count(func(e world.Entity) bool {
		ent, ok := e.(*entity.Ent)
		if !ok {
			return false
		}
		b, ok := ent.Behaviour().(*entity.ItemBehaviour)
		if !ok {
			return false
		}
		name, _ := b.Item().Item().EncodeItem()
		return name == "minecraft:torchflower_seeds" || name == "minecraft:pitcher_pod"
	})
	if seeds != 1 {
		t.Errorf("%d seed items dropped, want 1", seeds)
	}
	if _, ok := flagSeen(w, s, 200, protocol.EntityDataFlagTimerFlag2); !ok {
		t.Fatalf("never stands up: %v", w.Dump(s))
	}
	if !has(s, "minecraft:pushable_by_entity") || has(s, "minecraft:behavior.random_search_and_dig") {
		t.Errorf("after digging: %v", w.Dump(s))
	}
	if _, ok := w.TickUntil(120, func(tx *world.Tx) bool {
		m, _ := s.Mob(tx)
		return m != nil && !m.HasComponent("minecraft:behavior.timer_flag_2")
	}); !ok {
		t.Errorf("stand_up never ended: %v", w.Dump(s))
	}
}
