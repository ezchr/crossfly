//go:build crossfly_full

package jnether

import (
	"math/rand/v2"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestMain(m *testing.M) {
	if err := mobtest.RegisterTypes(); err != nil {
		panic(err)
	}
	javamob.Install()
	os.Exit(m.Run())
}

func spawnJ(t *testing.T, w *mobtest.World, id string, pos mgl64.Vec3) *mobtest.Ref {
	t.Helper()
	var ref *mobtest.Ref
	w.Do(func(tx *world.Tx) {
		m, err := javamob.Spawn(tx, id, pos, javamob.SpawnCommand, javamob.SpawnOptions{})
		if err != nil {
			t.Fatal(err)
		}
		ref = w.Track(m.H())
	})
	return ref
}

func withJ(t *testing.T, w *mobtest.World, r *mobtest.Ref, f func(m *javamob.Mob)) {
	t.Helper()
	w.Do(func(tx *world.Tx) {
		bm, ok := r.Mob(tx)
		if !ok {
			t.Fatal("mob gone")
		}
		m, ok := javamob.Of(bm)
		if !ok {
			t.Fatal("not a Java mob")
		}
		f(m)
	})
}

// targetIs reports whether m targets the entity of h.
func targetIs(m *javamob.Mob, h *world.EntityHandle) bool {
	t, ok := m.Target()
	return ok && t.H() == h
}

// TestZombifiedPiglinGroupAnger: hitting one angers the zombified piglins
// within its follow range (35) sideways and 10 up and down at once
// (HurtByTargetGoal.setAlertOthers); one 45 blocks away stays calm.
func TestZombifiedPiglinGroupAnger(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 60, block.Stone{}) })
	p := w.AddPlayer("tester", mgl64.Vec3{0.5, 1, 0.5})
	hit := spawnJ(t, w, ZombifiedPiglinID, mgl64.Vec3{3.5, 1, 0.5})
	near := spawnJ(t, w, ZombifiedPiglinID, mgl64.Vec3{33.5, 1, 0.5})
	far := spawnJ(t, w, ZombifiedPiglinID, mgl64.Vec3{48.5, 1, 0.5})
	w.Tick(5)
	for _, r := range []*mobtest.Ref{hit, near, far} {
		withJ(t, w, r, func(m *javamob.Mob) {
			if _, ok := m.Target(); ok {
				t.Fatal("a zombified piglin targets someone before being hurt")
			}
		})
	}
	withJ(t, w, hit, func(m *javamob.Mob) {
		e, _ := p.Entity(m.Tx())
		m.Hurt(1, entity.AttackDamageSource{Attacker: e})
	})
	w.Tick(4)
	withJ(t, w, hit, func(m *javamob.Mob) {
		if !targetIs(m, p.H()) {
			t.Error("the hit one is not after the player")
		}
		if !m.Behaviour().(*ZombifiedPiglin).IsAngry(m) {
			t.Error("the hit one is not angry")
		}
	})
	withJ(t, w, near, func(m *javamob.Mob) {
		if !targetIs(m, p.H()) {
			t.Error("the one 30 blocks away did not join in")
		}
	})
	withJ(t, w, far, func(m *javamob.Mob) {
		if _, ok := m.Target(); ok {
			t.Error("the one 45 blocks away got angry")
		}
	})
}

// TestZombifiedPiglinNeutral: a player nearby is left alone; angry ones
// forget the player once it died (forgiveDeadPlayers).
func TestZombifiedPiglinForgives(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 16, block.Stone{}) })
	p := w.AddPlayer("tester", mgl64.Vec3{0.5, 1, 0.5})
	z := spawnJ(t, w, ZombifiedPiglinID, mgl64.Vec3{4.5, 1, 0.5})
	w.Tick(40)
	withJ(t, w, z, func(m *javamob.Mob) {
		if _, ok := m.Target(); ok {
			t.Fatal("a calm zombified piglin attacks")
		}
		e, _ := p.Entity(m.Tx())
		m.Hurt(1, entity.AttackDamageSource{Attacker: e})
	})
	w.Tick(3)
	withJ(t, w, z, func(m *javamob.Mob) {
		if !m.Behaviour().(*ZombifiedPiglin).IsAngry(m) {
			t.Fatal("not angry after a hit")
		}
	})
	p.With(func(pl *player.Player) { pl.Hurt(1000, entity.VoidDamageSource{}) })
	w.Tick(3)
	withJ(t, w, z, func(m *javamob.Mob) {
		if m.Behaviour().(*ZombifiedPiglin).IsAngry(m) {
			t.Error("still angry at a dead player")
		}
	})
}

// TestPortalSpawnsZombifiedPiglin: Install makes portal random ticks spawn
// Java zombified piglins on the frame.
func TestPortalSpawnsZombifiedPiglin(t *testing.T) {
	Install()
	t.Cleanup(func() { portalHooks = PortalSpawnHooks{} })
	w := mobtest.New(t, mobtest.Options{Difficulty: world.DifficultyHard})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 3, 8, block.Stone{}) })
	w.AddPlayer("tester", mgl64.Vec3{6.5, 4, 6.5})
	w.Do(func(tx *world.Tx) {
		opts := &world.SetOpts{DisableBlockUpdates: true}
		tx.SetBlock(cube.Pos{0, 4, 0}, block.Obsidian{}, opts)
		for y := 5; y <= 7; y++ {
			tx.SetBlock(cube.Pos{0, y, 0}, block.Portal{Axis: cube.X}, opts)
		}
	})
	r := rand.New(rand.NewPCG(8, 9))
	var got *javamob.Mob
	w.Do(func(tx *world.Tx) {
		for range 20000 {
			portalRandomTick(cube.Pos{0, 6, 0}, tx, r)
			for e := range tx.EntitiesWithin(cube.Box(-2, 3, -2, 3, 9, 3)) {
				if m, ok := javamob.Of(e); ok && m.JavaType().ID == ZombifiedPiglinID {
					got = m
				}
			}
			if got != nil {
				break
			}
		}
		if got == nil {
			t.Fatal("no zombified piglin from 20000 portal ticks on hard")
		}
		if p := got.Position(); p[1] != 5 || p[0] != 0.5 || p[2] != 0.5 {
			t.Errorf("spawned at %v, want 0.5 5 0.5", p)
		}
	})
}

// TestBlazeVolley: a blaze that sees a player charges for 60 ticks and
// fires three small fireballs 6 ticks apart.
func TestBlazeVolley(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 24, block.Stone{}) })
	w.AddPlayer("tester", mgl64.Vec3{12.5, 1, 0.5})
	b := spawnJ(t, w, "minecraft:blaze", mgl64.Vec3{0.5, 1, 0.5})
	fired := map[*world.EntityHandle]int{}
	charged := false
	for i := 1; i <= 140; i++ {
		w.Tick(1)
		w.Do(func(tx *world.Tx) {
			for e := range tx.EntitiesWithin(cube.Box(-24, -5, -24, 24, 20, 24)) {
				if e.H().Type() == SmallFireballType {
					if _, ok := fired[e.H()]; !ok {
						fired[e.H()] = i
					}
				}
			}
		})
		withJ(t, w, b, func(m *javamob.Mob) {
			if m.Meta().Charged {
				charged = true
			}
		})
	}
	if !charged {
		t.Error("the blaze never charged")
	}
	if len(fired) != 3 {
		t.Fatalf("%d fireballs, want 3 in a volley", len(fired))
	}
	var ticks []int
	for _, at := range fired {
		ticks = append(ticks, at)
	}
	lo, hi := ticks[0], ticks[0]
	for _, v := range ticks {
		lo, hi = min(lo, v), max(hi, v)
	}
	if hi-lo != 12 {
		t.Errorf("volley spread over %d ticks, want 12 (6 apart)", hi-lo)
	}
}

// TestWitherSkeletonWithers: its hits give Wither; it holds a stone sword
// and hits for 4 (+ the difficulty bonus).
func TestWitherSkeletonWithers(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 16, block.Stone{}) })
	p := w.AddPlayer("tester", mgl64.Vec3{2.5, 1, 0.5})
	s := spawnJ(t, w, WitherSkeletonID, mgl64.Vec3{0.5, 1, 0.5})
	withJ(t, w, s, func(m *javamob.Mob) {
		if m.Attr(javamob.AttrAttackDamage) != 4 {
			t.Errorf("attack damage %v, want 4", m.Attr(javamob.AttrAttackDamage))
		}
	})
	withered := false
	for i := 0; i < 200 && !withered; i++ {
		w.Tick(1)
		p.With(func(pl *player.Player) {
			if _, ok := pl.Effect(effect.Wither); ok {
				withered = true
			}
		})
	}
	if !withered {
		t.Error("no Wither after 10 seconds next to a wither skeleton")
	}
}

// TestEndermiteLifetime: gone after 2400 ticks.
func TestEndermiteLifetime(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	e := spawnJ(t, w, "minecraft:endermite", mgl64.Vec3{0.5, 1, 0.5})
	withJ(t, w, e, func(m *javamob.Mob) { m.Behaviour().(*Endermite).life = 2395 })
	w.Tick(3)
	if e.Gone() {
		t.Fatal("gone too early")
	}
	w.Tick(5)
	if !e.Gone() {
		t.Error("still there after its lifetime")
	}
}

// TestPortalCandidates: the world tick hook finds the portal blocks near a
// player through the nether_portal points of interest.
func TestPortalCandidates(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{Difficulty: world.DifficultyHard})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 3, 8, block.Stone{}) })
	w.AddPlayer("tester", mgl64.Vec3{6.5, 4, 6.5})
	w.Do(func(tx *world.Tx) {
		opts := &world.SetOpts{DisableBlockUpdates: true}
		tx.SetBlock(cube.Pos{0, 4, 0}, block.Obsidian{}, opts)
		for y := 5; y <= 7; y++ {
			tx.SetBlock(cube.Pos{0, y, 0}, block.Portal{Axis: cube.X}, opts)
		}
	})
	w.Do(func(tx *world.Tx) {
		got := portalCandidates(tx)
		if len(got) != 3 {
			t.Fatalf("%d portal blocks found, want 3: %v", len(got), got)
		}
	})
}

// TestZombifiedPiglinTramplesTurtleEggs: zombified piglins walk to turtle
// eggs and destroy them (Zombie.ZombieAttackTurtleEggGoal; gold farms use
// eggs as bait).
func TestZombifiedPiglinTramplesTurtleEggs(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 16, block.Stone{})
		tx.SetBlock(cube.Pos{6, 1, 0}, block.TurtleEgg{}, nil)
	})
	spawnJ(t, w, ZombifiedPiglinID, mgl64.Vec3{0.5, 1, 0.5})
	gone := false
	for i := 0; i < 1600 && !gone; i += 20 {
		w.Tick(20)
		w.Do(func(tx *world.Tx) {
			_, egg := tx.Block(cube.Pos{6, 1, 0}).(block.TurtleEgg)
			gone = !egg
		})
	}
	if !gone {
		t.Error("the turtle egg survived 80 seconds next to a zombified piglin")
	}
}
