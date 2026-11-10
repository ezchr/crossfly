//go:build crossfly_full

package monster_test

import (
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/monster"
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

func spawn(t *testing.T, w *mobtest.World, id string, pos mgl64.Vec3, configure func(m *javamob.Mob)) *mobtest.Ref {
	t.Helper()
	var ref *mobtest.Ref
	w.Do(func(tx *world.Tx) {
		m, err := javamob.Spawn(tx, id, pos, javamob.SpawnCommand, javamob.SpawnOptions{Configure: configure})
		if err != nil {
			t.Fatal(err)
		}
		ref = w.Track(m.H())
	})
	return ref
}

func with(t *testing.T, w *mobtest.World, r *mobtest.Ref, f func(m *javamob.Mob)) {
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

// TestZombieChasesPlayer: a zombie targets a survival player, walks over and hits.
func TestZombieChasesPlayer(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 24, block.Stone{}) })
	p := w.AddPlayer("Steve", mgl64.Vec3{8.5, 1, 0.5})
	z := spawn(t, w, "minecraft:zombie", mgl64.Vec3{0.5, 1, 0.5}, nil)
	before := p.Health()
	w.Tick(300)
	if p.Health() >= before {
		t.Fatalf("player not hurt (health %v); zombie at %v", p.Health(), z.Pos())
	}
}

// TestZombieBurnsInDaylight: under the open sky at noon a zombie without a helmet catches fire.
func TestZombieBurnsInDaylight(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	z := spawn(t, w, "minecraft:zombie", mgl64.Vec3{0.5, 1, 0.5}, func(m *javamob.Mob) {
		m.Armour().SetHelmet(item.Stack{})
	})
	burnt := false
	for i := 0; i < 200 && !burnt; i++ {
		w.Tick(1)
		with(t, w, z, func(m *javamob.Mob) {
			m.Armour().SetHelmet(item.Stack{})
			burnt = m.OnFireDuration() > 0
		})
	}
	if !burnt {
		t.Fatal("zombie did not burn in daylight")
	}
}

// TestZombieDrownedConversion: 30 seconds with its eyes in water start the conversion (shaking), 15 more make it
// a drowned with the zombie's equipment.
func TestZombieDrownedConversion(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 6, block.Stone{})
		for x := -2; x <= 2; x++ {
			for z := -2; z <= 2; z++ {
				for y := 1; y <= 4; y++ {
					tx.SetBlock(cube.Pos{x, y, z}, block.Water{Still: true, Depth: 8}, nil)
				}
			}
		}
		for x := -3; x <= 3; x++ {
			for y := 1; y <= 5; y++ {
				tx.SetBlock(cube.Pos{x, y, -3}, block.Stone{}, nil)
				tx.SetBlock(cube.Pos{x, y, 3}, block.Stone{}, nil)
				tx.SetBlock(cube.Pos{-3, y, x}, block.Stone{}, nil)
				tx.SetBlock(cube.Pos{3, y, x}, block.Stone{}, nil)
			}
		}
		// A lid so the zombie cannot swim up and breathe.
		for x := -2; x <= 2; x++ {
			for z := -2; z <= 2; z++ {
				tx.SetBlock(cube.Pos{x, 5, z}, block.Stone{}, nil)
			}
		}
	})
	z := spawn(t, w, "minecraft:zombie", mgl64.Vec3{0.5, 1, 0.5}, func(m *javamob.Mob) {
		m.SetItemSlot(javamob.SlotMainHand, item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1))
	})
	w.Tick(605)
	shaking := false
	with(t, w, z, func(m *javamob.Mob) { shaking = m.Meta().Shaking })
	if !shaking {
		t.Fatal("zombie not converting after 30 seconds under water")
	}
	w.Tick(310)
	if !z.Gone() {
		t.Fatal("zombie still there after the conversion time")
	}
	drowned := w.Mobs("minecraft:drowned")
	if len(drowned) != 1 {
		t.Fatalf("%d drowned, want 1", len(drowned))
	}
}

// TestBabyZombie: a baby zombie is small and 50% faster.
func TestBabyZombie(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 6, block.Stone{}) })
	z := spawn(t, w, "minecraft:zombie", mgl64.Vec3{0.5, 1, 0.5}, nil)
	with(t, w, z, func(m *javamob.Mob) {
		adult := m.Attr(javamob.AttrMovementSpeed)
		m.SetBaby(true)
		if m.Width() > 0.5 || m.Height() > 1 {
			t.Fatalf("baby dimensions %v x %v", m.Width(), m.Height())
		}
		if got := m.Attr(javamob.AttrMovementSpeed); got < adult*1.49 {
			t.Fatalf("baby speed %v, adult %v", got, adult)
		}
	})
}

// TestSkeletonShoots: a skeleton with its bow shoots arrows at a player in range.
func TestSkeletonShoots(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 24, block.Stone{}) })
	w.AddPlayer("Alex", mgl64.Vec3{10.5, 1, 0.5})
	s := spawn(t, w, "minecraft:skeleton", mgl64.Vec3{0.5, 1, 0.5}, nil)
	with(t, w, s, func(m *javamob.Mob) {
		if main, _ := m.HeldItems(); main.Empty() {
			t.Fatal("skeleton spawned without a bow")
		}
	})
	n, ok := w.TickUntil(200, func(tx *world.Tx) bool {
		for e := range tx.Entities() {
			if e.H().Type() == entity.ArrowType {
				return true
			}
		}
		return false
	})
	if !ok {
		t.Fatal("no arrow shot in 10 seconds")
	}
	_ = n
}

// TestCreeperExplodes: a creeper next to a player swells for 1.5 seconds and explodes, breaking blocks.
func TestCreeperExplodes(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 12, block.Dirt{}) })
	w.AddPlayer("Steve", mgl64.Vec3{2.5, 1, 0.5})
	c := spawn(t, w, "minecraft:creeper", mgl64.Vec3{0.5, 1, 0.5}, nil)
	w.Tick(120)
	if !c.Gone() {
		t.Fatal("creeper did not explode")
	}
	holes := 0
	w.Do(func(tx *world.Tx) {
		for x := -2; x <= 3; x++ {
			if _, air := tx.Block(cube.Pos{x, 0, 0}).(block.Air); air {
				holes++
			}
		}
	})
	if holes == 0 {
		t.Fatal("the explosion broke no blocks")
	}
}

// TestSpiderClimbs: a spider walking into a wall towards its target climbs it.
func TestSpiderClimbs(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 12, block.Stone{})
		for z := -3; z <= 3; z++ {
			for y := 1; y <= 4; y++ {
				tx.SetBlock(cube.Pos{3, y, z}, block.Stone{}, nil)
			}
		}
	})
	s := spawn(t, w, "minecraft:spider", mgl64.Vec3{0.5, 1, 0.5}, nil)
	maxY := 0.0
	for i := 0; i < 100; i++ {
		with(t, w, s, func(m *javamob.Mob) { m.MoveControl().SetWantedPosition(5.5, 5, 0.5, 1) })
		w.Tick(1)
		maxY = max(maxY, s.Pos()[1])
	}
	if maxY < 3 {
		t.Fatalf("spider climbed to y %v, want above 3", maxY)
	}
}

// TestEndermanDodgesArrows: an arrow does not hurt an enderman; it teleports away.
func TestEndermanDodgesArrows(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 40, block.Stone{}) })
	e := spawn(t, w, "minecraft:enderman", mgl64.Vec3{0.5, 1, 0.5}, nil)
	w.Tick(2)
	with(t, w, e, func(m *javamob.Mob) {
		before := m.Health()
		pos := m.Position()
		m.Hurt(5, entity.ProjectileDamageSource{})
		if m.Health() != before {
			t.Fatalf("enderman hurt by a projectile: %v -> %v", before, m.Health())
		}
		if m.Position() == pos {
			t.Fatal("enderman did not teleport")
		}
	})
}

// TestEndermanHurtByWater: an enderman in water takes damage.
func TestEndermanHurtByWater(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 6, block.Stone{})
		tx.SetBlock(cube.Pos{0, 1, 0}, block.Water{Still: true, Depth: 8}, nil)
	})
	e := spawn(t, w, "minecraft:enderman", mgl64.Vec3{0.5, 1, 0.5}, nil)
	var before float64
	with(t, w, e, func(m *javamob.Mob) { before = m.Health() })
	w.Tick(3)
	if e.Gone() {
		return
	}
	with(t, w, e, func(m *javamob.Mob) {
		if m.Health() >= before && m.Position()[0] == 0.5 {
			t.Fatal("enderman in water neither hurt nor teleported")
		}
	})
}

var _ = monster.NewZombie
