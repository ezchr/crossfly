//go:build crossfly_full

package core_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	. "github.com/df-mc/dragonfly/server/mob/component/core/coretest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// A leashable test mob: the llama's leashable (on_leash/on_unleash events
// that add and remove a group) and the camel's presets (a closer leash when
// a happy ghast holds it).
var leashMob = &Def{ID: "test:leashed", Category: "creature",
	Base: []mob.RawComponent{
		C("minecraft:type_family", `{"family":["llama","mob"]}`),
		C("minecraft:health", `{"value":10,"max":10}`),
		C("minecraft:movement", `{"value":0.25}`),
		C("minecraft:navigation.walk", `{}`),
		C("minecraft:leashable", `{"on_leash":{"event":"minecraft:on_leash","target":"self"},"on_unleash":{"event":"minecraft:on_unleash","target":"self"},"presets":[{"filter":{"subject":"other","test":"is_family","value":"happy_ghast"},"hard_distance":10,"max_distance":14}]}`),
	},
	Groups: map[string][]mob.RawComponent{
		"unleashed": {C("minecraft:is_chested", `{}`)},
	},
	Events: map[string]Event{
		mob.EventSpawned:       {Add: []string{"unleashed"}},
		"minecraft:on_leash":   {Remove: []string{"unleashed"}},
		"minecraft:on_unleash": {Add: []string{"unleashed"}},
	},
}

var ghast = &Def{ID: "test:ghast", Category: "creature",
	Base: []mob.RawComponent{
		C("minecraft:type_family", `{"family":["happy_ghast","mob"]}`),
		C("minecraft:health", `{"value":20,"max":20}`),
	},
}

func init() {
	item.RegisterMobItems() // the lead
	Register(leashMob, ghast)
}

var lead = item.NewStack(item.Lead{}, 1)

func leads(tx *world.Tx) int {
	n := 0
	for _, s := range Items(tx) {
		if _, ok := s.Item().(item.Lead); ok {
			n += s.Count()
		}
	}
	return n
}

func holderOf(t *testing.T, m *mob.Mob) world.Entity {
	t.Helper()
	h, ok := core.LeashHolder(m)
	if !ok {
		Fatalf(t, "%v is not leashed", m.Identifier())
	}
	return h
}

// TestLeashAttachAndUnleash: a player with a lead leashes the mob (the lead
// is used up, on_leash fires, the leashed flag and the holder data are set);
// the holder using it again unleashes it and the lead drops.
func TestLeashAttachAndUnleash(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx)
		m, _ := Spawn(t, tx, "test:leashed", at, "")
		p := Player(tx, at.Add(mgl64.Vec3{2, 0, 0}), lead)
		if _, ok := m.Interact(p, item.NewStack(item.Stick{}, 1), m.Position()); ok {
			Fatal(t, "a stick leashed the mob")
		}
		left, ok := m.Interact(p, lead, m.Position())
		if !ok || !left.Empty() {
			Fatalf(t, "leash: used %v, left %v", ok, left)
		}
		if !core.Leashed(m) || holderOf(t, m).H() != p.H() {
			Fatal(t, "not leashed to the player")
		}
		if InstOf(m).HasGroup("unleashed") {
			Fatal(t, "on_leash did not fire")
		}
		if !m.ActorFlag(protocol.EntityDataFlagLeashed) {
			Fatal(t, "leashed flag not set")
		}
		if v, _ := m.ActorData(protocol.EntityDataKeyLeashHolder); v != p.H() {
			Fatalf(t, "holder data %v", v)
		}
		// Another lead does nothing on a leashed mob (not stealable).
		other := Player(tx, at.Add(mgl64.Vec3{-2, 0, 0}), lead)
		if _, ok := m.Interact(other, lead, m.Position()); ok {
			Fatal(t, "a second player took the leash")
		}
		// The holder unleashes it with an empty hand: the lead drops.
		if _, ok := m.Interact(p, item.Stack{}, m.Position()); !ok {
			Fatal(t, "the holder could not unleash the mob")
		}
		if core.Leashed(m) || m.ActorFlag(protocol.EntityDataFlagLeashed) {
			Fatal(t, "still leashed")
		}
		if v, _ := m.ActorData(protocol.EntityDataKeyLeashHolder); v != int64(-1) {
			Fatalf(t, "holder data after unleashing %v, want -1", v)
		}
		if !InstOf(m).HasGroup("unleashed") {
			Fatal(t, "on_unleash did not fire")
		}
		if n := leads(tx); n != 1 {
			Fatalf(t, "%d leads dropped, want 1", n)
		}
	})
}

// TestLeashCreativeKeepsLead: in creative the lead is not used up.
func TestLeashCreativeKeepsLead(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx)
		m, _ := Spawn(t, tx, "test:leashed", at, "")
		p := Player(tx, at.Add(mgl64.Vec3{2, 0, 0}), lead)
		p.SetGameMode(world.GameModeCreative)
		left, ok := m.Interact(p, lead, m.Position())
		if !ok || left.Count() != 1 || !core.Leashed(m) {
			Fatalf(t, "creative leash: %v %v", ok, left)
		}
	})
}

// TestLeashPullAndBreak: beyond soft_distance the mob paths to the holder,
// beyond hard_distance it is pulled, beyond max_distance the leash breaks
// and the lead drops (LeashableSystem).
func TestLeashPullAndBreak(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx)
		m, c := Spawn(t, tx, "test:leashed", at, "")
		p := Player(tx, at.Add(mgl64.Vec3{3, 0, 0}), lead)
		m.Interact(p, lead, m.Position())
		TickAll(tx, 1, m)
		if c.Nav.MoveTos != 0 {
			Fatalf(t, "pathed to a holder within soft_distance (%d)", c.Nav.MoveTos)
		}
		p.Teleport(m.Position().Add(mgl64.Vec3{8, 0, 0}))
		m.SetVelocity(mgl64.Vec3{})
		c.Nav.Fail = true // the fake would walk the mob: only the pull moves it
		TickAll(tx, 1, m)
		if c.Nav.MoveTos == 0 {
			Fatal(t, "no path to a holder beyond soft_distance")
		}
		if v := m.Vel(); v[0] <= 0.05 {
			Fatalf(t, "not pulled towards the holder beyond hard_distance: velocity %v", v)
		}
		if !core.Leashed(m) {
			Fatal(t, "leash broke within max_distance")
		}
		p.Teleport(m.Position().Add(mgl64.Vec3{11, 0, 0}))
		TickAll(tx, 1, m)
		if core.Leashed(m) {
			Fatal(t, "leash did not break beyond max_distance")
		}
		if n := leads(tx); n != 1 {
			Fatalf(t, "%d leads dropped, want 1", n)
		}
	})
}

// TestLeashHolderGone: a holder that dies breaks the leash.
func TestLeashHolderGone(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx)
		m, _ := Spawn(t, tx, "test:leashed", at, "")
		g, _ := Spawn(t, tx, "test:ghast", at.Add(mgl64.Vec3{2, 0, 0}), "")
		core.Leash(m, g)
		TickAll(tx, 1, m)
		if !core.Leashed(m) {
			Fatal(t, "leash to a mob broke")
		}
		g.Hurt(1000, entity.VoidDamageSource{})
		TickAll(tx, 1, m)
		if core.Leashed(m) || leads(tx) != 1 {
			Fatalf(t, "leash kept to a dead holder (leads %d)", leads(tx))
		}
	})
}

// TestLeashPresets: the first preset whose filter passes for the holder sets
// the distances: a happy ghast holds the mob at up to 14 blocks.
func TestLeashPresets(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx)
		m, _ := Spawn(t, tx, "test:leashed", at, "")
		g, _ := Spawn(t, tx, "test:ghast", at.Add(mgl64.Vec3{12, 0, 0}), "")
		core.Leash(m, g)
		TickAll(tx, 1, m)
		if !core.Leashed(m) {
			Fatal(t, "the happy ghast preset (max 14) was not used")
		}
		p := Player(tx, at.Add(mgl64.Vec3{1, 0, 0}), lead)
		core.DropLeash(m, false, false)
		core.Leash(m, p)
		p.Teleport(m.Position().Add(mgl64.Vec3{12, 0, 0}))
		TickAll(tx, 1, m)
		if core.Leashed(m) {
			Fatal(t, "a player held the mob beyond the default max_distance")
		}
	})
}

// TestLeashDeath: a leashed mob that dies drops its lead.
func TestLeashDeath(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx)
		m, _ := Spawn(t, tx, "test:leashed", at, "")
		p := Player(tx, at.Add(mgl64.Vec3{2, 0, 0}), lead)
		m.Interact(p, lead, m.Position())
		m.Hurt(1000, entity.VoidDamageSource{})
		if leads(tx) != 1 {
			Fatalf(t, "%d leads dropped on death, want 1", leads(tx))
		}
	})
}

func knots(tx *world.Tx) []*core.Knot {
	var out []*core.Knot
	for e := range tx.Entities() {
		if k, ok := e.(*core.Knot); ok {
			out = append(out, k)
		}
	}
	return out
}

// TestFenceKnot: a lead used on a fence ties the player's mobs to a knot;
// using the knot removes it and the mobs drop their leads; a knot whose
// fence is gone goes within 100 ticks; the last mob unleashed takes the knot
// away.
func TestFenceKnot(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx)
		fence := cube.Pos{3, 10, 0}
		tx.SetBlock(fence, block.WoodFence{Wood: block.OakWood()}, nil)
		m, _ := Spawn(t, tx, "test:leashed", at, "")
		m2, _ := Spawn(t, tx, "test:leashed", at.Add(mgl64.Vec3{0, 0, 1}), "")
		p := Player(tx, at.Add(mgl64.Vec3{1, 0, 0}), lead)
		m.Interact(p, lead, m.Position())
		m2.Interact(p, lead, m2.Position())
		ctx := &item.UseContext{}
		if !(item.Lead{}).UseOnBlock(fence, cube.FaceUp, mgl64.Vec3{}, tx, p, ctx) || ctx.CountSub != 0 {
			Fatalf(t, "lead on a fence: sub %d", ctx.CountSub)
		}
		ks := knots(tx)
		if len(ks) != 1 || cube.PosFromVec3(ks[0].Position()) != fence {
			Fatalf(t, "knots %v", ks)
		}
		for _, mm := range []*mob.Mob{m, m2} {
			if holderOf(t, mm).H() != ks[0].H() {
				Fatal(t, "mob not tied to the knot")
			}
		}
		if InstOf(m).HasGroup("unleashed") {
			Fatal(t, "tying to the fence fired on_unleash")
		}
		// The last of the two unleashed takes the knot away.
		core.DropLeash(m, true, false)
		if len(knots(tx)) != 1 {
			Fatal(t, "knot went with a mob still tied")
		}
		core.DropLeash(m2, true, false)
		if len(knots(tx)) != 0 {
			Fatal(t, "knot stayed without mobs")
		}

		// Using the knot removes it.
		m.Interact(p, lead, m.Position())
		if !core.BindPlayerMobs(tx, p, fence) {
			Fatal(t, "fence use tied nothing")
		}
		k := knots(tx)[0]
		k.Interact(p, item.Stack{}, k.Position())
		if core.Leashed(m) || len(knots(tx)) != 0 {
			Fatal(t, "knot use did not free the mob")
		}

		// A knot without its fence goes.
		m.Interact(p, lead, m.Position())
		core.BindPlayerMobs(tx, p, fence)
		k = knots(tx)[0]
		tx.SetBlock(fence, block.Air{}, nil)
		for i := 0; i < 102; i++ {
			if kk, ok := k.H().Entity(tx); ok {
				kk.(*core.Knot).Tick(tx, int64(i))
			}
		}
		if core.Leashed(m) || len(knots(tx)) != 0 {
			Fatal(t, "knot outlived its fence")
		}
	})
}

// TestBindNeedsLeashedMobs: a fence used without led mobs makes no knot.
func TestBindNeedsLeashedMobs(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx)
		fence := cube.Pos{3, 10, 0}
		tx.SetBlock(fence, block.WoodFence{Wood: block.OakWood()}, nil)
		Spawn(t, tx, "test:leashed", at, "")
		p := Player(tx, at.Add(mgl64.Vec3{1, 0, 0}), lead)
		if core.BindPlayerMobs(tx, p, fence) || len(knots(tx)) != 0 {
			Fatal(t, "knot without mobs")
		}
	})
}
