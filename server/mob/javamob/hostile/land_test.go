//go:build crossfly_full

package hostile

import (
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
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

// entityAttack is a melee hit by e.
func entityAttack(e world.Entity) world.DamageSource { return entity.AttackDamageSource{Attacker: e} }

// hasEffect reports whether the test player has an effect.
func hasEffect(p *mobtest.Player, t effect.Type) bool {
	found := false
	p.With(func(pl *player.Player) { _, found = pl.Effect(t) })
	return found
}

// TestHuskStarvesAndDoesNotBurn: a husk at noon stays unlit, walks to the
// player and its bare-handed hit gives Hunger.
func TestHuskStarvesAndDoesNotBurn(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 24, block.Stone{}) })
	p := w.AddPlayer("tester", mgl64.Vec3{6.5, 1, 0.5})
	h := spawnJ(t, w, huskID, mgl64.Vec3{0.5, 1, 0.5})
	withJ(t, w, h, func(m *javamob.Mob) { m.SetHeldItems(item.Stack{}, item.Stack{}) })
	n, ok := w.TickUntil(400, func(*world.Tx) bool { return hasEffect(p, effect.Hunger) })
	if !ok {
		t.Fatalf("no hunger after 20 s (player health %v)", p.Health())
	}
	t.Logf("hunger after %d ticks", n)
	withJ(t, w, h, func(m *javamob.Mob) {
		if m.IsOnFire() {
			t.Errorf("a husk burns in daylight")
		}
	})
}

// TestStraySlownessArrow: a stray's arrow slows the player for 30 seconds.
func TestStraySlownessArrow(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 24, block.Stone{}) })
	p := w.AddPlayer("tester", mgl64.Vec3{8.5, 1, 0.5})
	spawnJ(t, w, strayID, mgl64.Vec3{0.5, 1, 0.5})
	n, ok := w.TickUntil(400, func(*world.Tx) bool { return hasEffect(p, effect.Slowness) })
	if !ok {
		t.Fatalf("not slowed after 20 s (health %v)", p.Health())
	}
	t.Logf("slowed after %d ticks", n)
	p.With(func(pl *player.Player) {
		e, _ := pl.Effect(effect.Slowness)
		if d := e.Duration(); d < ticks(560) || d > ticks(600) {
			t.Errorf("slowness lasts %v, want about 30 s", d)
		}
	})
}

// TestCaveSpiderPoisons: on normal a cave spider's bite poisons for 7 s.
func TestCaveSpiderPoisons(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 24, block.Stone{}) })
	p := w.AddPlayer("tester", mgl64.Vec3{4.5, 1, 0.5})
	spawnJ(t, w, caveSpiderID, mgl64.Vec3{0.5, 1, 0.5})
	if _, ok := w.TickUntil(400, func(*world.Tx) bool { return hasEffect(p, effect.Poison) }); !ok {
		t.Fatalf("not poisoned after 20 s (health %v)", p.Health())
	}
	p.With(func(pl *player.Player) {
		e, _ := pl.Effect(effect.Poison)
		if d := e.Duration(); d > ticks(140) || d < ticks(120) {
			t.Errorf("poison lasts %v, want 7 s", d)
		}
	})
}

// TestBoggedShearing: shears on a bogged shear it once (its two mushrooms
// drop where the items exist: Crossfly has no mushroom items yet).
func TestBoggedShearing(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	p := w.AddPlayer("tester", mgl64.Vec3{1.5, 1, 0.5})
	p.SetGameMode(world.GameModeCreative)
	b := spawnJ(t, w, boggedID, mgl64.Vec3{0.5, 1, 0.5})
	if _, ok := p.Interact(b, item.NewStack(item.Shears{}, 1)); !ok {
		t.Fatal("shearing a bogged did nothing")
	}
	if _, ok := world.ItemByName("minecraft:red_mushroom", 0); ok {
		if n := w.Count(func(e world.Entity) bool { return e.H().Type().EncodeEntity() == "minecraft:item" }); n != 2 {
			t.Errorf("%d items dropped, want 2 mushrooms", n)
		}
	}
	withJ(t, w, b, func(m *javamob.Mob) {
		if !m.Meta().Sheared {
			t.Errorf("not shown as sheared")
		}
	})
	if _, ok := p.Interact(b, item.NewStack(item.Shears{}, 1)); ok {
		t.Errorf("a sheared bogged was sheared again")
	}
}

// TestSilverfishWakesFriends: hurting a silverfish breaks the infested blocks
// near it and lets their silverfish out.
func TestSilverfishWakesFriends(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 16, block.Dirt{}) // not a host block: the freed silverfish cannot hide again
		for x := 3; x <= 5; x++ {
			tx.SetBlock(cube.Pos{x, 1, 3}, block.InfestedStone{}, nil)
		}
	})
	s := spawnJ(t, w, silverfishID, mgl64.Vec3{0.5, 1, 0.5})
	p := w.AddPlayer("tester", mgl64.Vec3{1.5, 1, 0.5})
	w.Do(func(tx *world.Tx) {
		bm, _ := s.Mob(tx)
		pe, _ := p.Entity(tx)
		bm.Hurt(1, entityAttack(pe))
	})
	w.Tick(40)
	left := 0
	w.Do(func(tx *world.Tx) {
		for x := 3; x <= 5; x++ {
			if _, ok := tx.Block(cube.Pos{x, 1, 3}).(block.InfestedStone); ok {
				left++
			}
		}
	})
	if left == 3 {
		t.Fatalf("no infested block woke up")
	}
	if n := len(w.Mobs(silverfishID)); n < 2 {
		t.Errorf("%d silverfish, want the hurt one and at least one friend", n)
	}
}

// TestSilverfishHides: a silverfish with no target next to stone hides in
// it: the stone becomes infested and the silverfish is gone.
func TestSilverfishHides(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 6, block.Stone{}) })
	s := spawnJ(t, w, silverfishID, mgl64.Vec3{0.5, 1, 0.5})
	n, ok := w.TickUntil(2400, func(*world.Tx) bool { return s.Gone() })
	if !ok {
		t.Fatalf("the silverfish never hid")
	}
	t.Logf("hid after %d ticks", n)
	infested := 0
	w.Do(func(tx *world.Tx) {
		for x := -6; x <= 6; x++ {
			for z := -6; z <= 6; z++ {
				for y := -1; y <= 2; y++ {
					if _, ok := tx.Block(cube.Pos{x, y, z}).(block.InfestedStone); ok {
						infested++
					}
				}
			}
		}
	})
	if infested != 1 {
		t.Errorf("%d infested blocks, want 1", infested)
	}
}

// TestGiantIdle: a giant spawns with 100 health and no goals.
func TestGiantIdle(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	g := spawnJ(t, w, giantID, mgl64.Vec3{0.5, 1, 0.5})
	withJ(t, w, g, func(m *javamob.Mob) {
		if m.MaxHealth() != GiantHealth || m.Attr(javamob.AttrAttackDamage) != GiantAttack {
			t.Errorf("giant health %v attack %v", m.MaxHealth(), m.Attr(javamob.AttrAttackDamage))
		}
	})
}
