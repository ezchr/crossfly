package block_test

import (
	"context"
	"math/rand/v2"
	"sync"
	"testing"
	_ "unsafe"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/creative"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// berryUser is a user holding an item that places blocks straight into tx.
type berryUser struct {
	held item.Stack
	tx   *world.Tx
}

func (u *berryUser) Close() error                        { return nil }
func (u *berryUser) H() *world.EntityHandle              { return nil }
func (u *berryUser) Position() mgl64.Vec3                { return mgl64.Vec3{} }
func (u *berryUser) Rotation() cube.Rotation             { return cube.Rotation{} }
func (u *berryUser) HeldItems() (item.Stack, item.Stack) { return u.held, item.Stack{} }
func (u *berryUser) SetHeldItems(m, _ item.Stack)        { u.held = m }
func (u *berryUser) UsingItem() bool                     { return false }
func (u *berryUser) ReleaseItem()                        {}
func (u *berryUser) UseItem()                            {}
func (u *berryUser) PlaceBlock(pos cube.Pos, b world.Block, ctx *item.UseContext) {
	u.tx.SetBlock(pos, b, nil)
	ctx.CountSub++
}

// berryMob is a living entity walking through blocks.
type berryMob struct {
	world.Entity
	vel  mgl64.Vec3
	hurt float64
	src  world.DamageSource
}

func (m *berryMob) Velocity() mgl64.Vec3     { return m.vel }
func (m *berryMob) SetVelocity(v mgl64.Vec3) { m.vel = v }
func (m *berryMob) Hurt(d float64, src world.DamageSource) (float64, world.HurtResult) {
	m.hurt, m.src = m.hurt+d, src
	return d, world.HurtAccepted
}

// berryWorld returns a synchronous world closed at the end of the test.
func berryWorld(t *testing.T) *world.World {
	t.Helper()
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

// berryDo runs f in a transaction of w.
func berryDo(t *testing.T, w *world.World, f func(tx *world.Tx)) {
	t.Helper()
	if err := w.Do(f).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// berryDrops counts the items of type it lying in the world, removing them.
func berryDrops(tx *world.Tx, it world.Item) int {
	n := 0
	for e := range tx.Entities() {
		ent, ok := e.(*entity.Ent)
		if !ok {
			continue
		}
		if ib, ok := ent.Behaviour().(*entity.ItemBehaviour); ok && ib.Item().Item() == it {
			n += ib.Item().Count()
			_ = e.Close()
		}
	}
	return n
}

var berryCreativeOnce sync.Once

// berryInCreative reports whether an item of the type of it is in the creative inventory.
func berryInCreative(it world.Item) bool {
	berryCreativeOnce.Do(berryCreativeRegister)
	for _, c := range creative.Items() {
		if c.Stack.Item() == it {
			return true
		}
	}
	return false
}

//go:linkname berryCreativeRegister github.com/df-mc/dragonfly/server/item/creative.registerCreativeItems
func berryCreativeRegister()

// TestSweetBerryBushStates checks that the 8 growth states are Bedrock states, that sweet berries are the item
// of the bush, and that they are in the creative inventory.
func TestSweetBerryBushStates(t *testing.T) {
	berryWorld(t)
	for age := 0; age <= 7; age++ {
		b, ok := world.BlockByName("minecraft:sweet_berry_bush", map[string]any{"growth": int32(age)})
		if want := (block.SweetBerryBush{Age: age}); !ok || b != want {
			t.Errorf("growth %d: got %v (ok=%v), want %v", age, b, ok, want)
		}
	}
	if it, ok := world.ItemByName("minecraft:sweet_berries", 0); !ok || it != (block.SweetBerryBush{}) {
		t.Errorf("sweet berries item: %v %v", it, ok)
	}
	if !berryInCreative(block.SweetBerryBush{}) {
		t.Error("sweet berries are not in the creative inventory")
	}
	if c := (block.SweetBerryBush{}).CompostChance(); c != 0.3 {
		t.Errorf("compost chance %v, want 0.3", c)
	}
}

// TestSweetBerryBushPlacing: sweet berries plant a bush of age 0 on grass but not on stone, and the bush breaks
// when its soil goes.
func TestSweetBerryBushPlacing(t *testing.T) {
	w := berryWorld(t)
	berryDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 0, 0}, block.Grass{}, nil)
		tx.SetBlock(cube.Pos{2, 0, 0}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{4, 0, 0}, block.Podzol{}, nil)
		u := &berryUser{held: item.NewStack(block.SweetBerryBush{}, 8), tx: tx}
		use := func(p cube.Pos) bool {
			return block.SweetBerryBush{}.UseOnBlock(p, cube.FaceUp, mgl64.Vec3{}, tx, u, &item.UseContext{})
		}
		if use(cube.Pos{2, 0, 0}) {
			t.Error("a bush went on stone")
		}
		if !use(cube.Pos{0, 0, 0}) || tx.Block(cube.Pos{0, 1, 0}) != (block.SweetBerryBush{}) {
			t.Errorf("planting on grass: %v", tx.Block(cube.Pos{0, 1, 0}))
		}
		if !use(cube.Pos{4, 0, 0}) {
			t.Error("no bush on podzol")
		}
		tx.SetBlock(cube.Pos{0, 0, 0}, block.Stone{}, nil)
		block.SweetBerryBush{}.NeighbourUpdateTick(cube.Pos{0, 1, 0}, cube.Pos{0, 0, 0}, tx)
		if _, ok := tx.Block(cube.Pos{0, 1, 0}).(block.Air); !ok {
			t.Errorf("the bush stayed without soil: %v", tx.Block(cube.Pos{0, 1, 0}))
		}
	})
}

// TestSweetBerryBushGrowth: random ticks grow the bush one stage, 1 in 5, up to age 3 (no light needed), and bone
// meal grows it one stage.
func TestSweetBerryBushGrowth(t *testing.T) {
	w := berryWorld(t)
	pos := cube.Pos{0, 1, 0}
	berryDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 0, 0}, block.Grass{}, nil)
		tx.SetBlock(pos, block.SweetBerryBush{}, nil)
		r := rand.New(rand.NewPCG(3, 4))
		grown := 0
		for i := 0; i < 1000; i++ {
			before := tx.Block(pos).(block.SweetBerryBush)
			before.RandomTick(pos, tx, r)
			after := tx.Block(pos).(block.SweetBerryBush)
			if after.Age != before.Age {
				if after.Age != before.Age+1 {
					t.Fatalf("grew %d -> %d", before.Age, after.Age)
				}
				grown++
			}
		}
		if got := tx.Block(pos).(block.SweetBerryBush).Age; got != 3 || grown != 3 {
			t.Errorf("after 1000 random ticks: age %d, %d steps, want 3", got, grown)
		}

		tx.SetBlock(pos, block.SweetBerryBush{Age: 1}, nil)
		if res := (block.SweetBerryBush{Age: 1}).BoneMeal(pos, tx); res != item.BoneMealResultSmall || tx.Block(pos) != (block.SweetBerryBush{Age: 2}) {
			t.Errorf("bone meal on age 1: %v, %v", res, tx.Block(pos))
		}
		if res := (block.SweetBerryBush{Age: 3}).BoneMeal(pos, tx); res != item.BoneMealResultNone {
			t.Errorf("bone meal on a ripe bush: %v", res)
		}
	})
}

// TestSweetBerryBushPicking: using a bush with berries drops 1-2 (age 2) or 2-3 (age 3) and sets it back to age
// 1; younger bushes have nothing to pick; held bone meal fertilises an unripe bush instead of picking it.
func TestSweetBerryBushPicking(t *testing.T) {
	w := berryWorld(t)
	pos := cube.Pos{0, 1, 0}
	berryDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 0, 0}, block.Grass{}, nil)
		u := &berryUser{tx: tx}
		for _, c := range []struct{ age, lo, hi int }{{2, 1, 2}, {3, 2, 3}} {
			seen := map[int]bool{}
			for i := 0; i < 40; i++ {
				b := block.SweetBerryBush{Age: c.age}
				tx.SetBlock(pos, b, nil)
				if !b.Activate(pos, cube.FaceUp, tx, u, &item.UseContext{}) {
					t.Fatalf("picking age %d failed", c.age)
				}
				if got := tx.Block(pos); got != (block.SweetBerryBush{Age: 1}) {
					t.Fatalf("after picking age %d: %v, want age 1", c.age, got)
				}
				n := berryDrops(tx, block.SweetBerryBush{})
				if n < c.lo || n > c.hi {
					t.Fatalf("age %d dropped %d berries, want %d-%d", c.age, n, c.lo, c.hi)
				}
				seen[n] = true
			}
			if len(seen) != 2 {
				t.Errorf("age %d berry counts %v, want both of %d-%d", c.age, seen, c.lo, c.hi)
			}
		}
		for _, age := range []int{0, 1} {
			b := block.SweetBerryBush{Age: age}
			tx.SetBlock(pos, b, nil)
			if b.Activate(pos, cube.FaceUp, tx, u, &item.UseContext{}) {
				t.Errorf("picked a bush of age %d", age)
			}
		}
		u.held = item.NewStack(item.BoneMeal{}, 1)
		tx.SetBlock(pos, block.SweetBerryBush{Age: 2}, nil)
		if (block.SweetBerryBush{Age: 2}).Activate(pos, cube.FaceUp, tx, u, &item.UseContext{}) {
			t.Error("bone meal in hand picked an unripe bush")
		}
		if !(block.SweetBerryBush{Age: 3}).Activate(pos, cube.FaceUp, tx, u, &item.UseContext{}) {
			t.Error("bone meal in hand did not pick a ripe bush")
		}
		berryDrops(tx, block.SweetBerryBush{})
	})
}

// TestSweetBerryBushDrops: breaking drops berries by age (none before age 2), and the bush is raidable with
// berries; raiding picks them.
func TestSweetBerryBushDrops(t *testing.T) {
	for age, want := range [][2]int{{0, 0}, {0, 0}, {1, 2}, {2, 3}, {2, 3}} {
		drops := block.SweetBerryBush{Age: age}.BreakInfo().Drops(item.ToolNone{}, nil)
		n := 0
		for _, d := range drops {
			n += d.Count()
		}
		if n < want[0] || n > want[1] {
			t.Errorf("age %d drops %d, want %d-%d", age, n, want[0], want[1])
		}
		if got := (block.SweetBerryBush{Age: age}).Raidable(); got != (age >= 2) {
			t.Errorf("age %d raidable %v", age, got)
		}
	}
	w := berryWorld(t)
	pos := cube.Pos{0, 1, 0}
	berryDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 0, 0}, block.Grass{}, nil)
		tx.SetBlock(pos, block.SweetBerryBush{Age: 3}, nil)
		block.SweetBerryBush{Age: 3}.Raid(pos, tx, nil)
		if tx.Block(pos) != (block.SweetBerryBush{Age: 1}) || berryDrops(tx, block.SweetBerryBush{}) < 2 {
			t.Errorf("raiding: %v", tx.Block(pos))
		}
	})
}

// TestSweetBerryBushInside: a bush hurts a mob moving in it for 1 contact damage of the bush and slows it down;
// saplings and still mobs are not hurt.
func TestSweetBerryBushInside(t *testing.T) {
	for _, c := range []struct {
		age  int
		vel  mgl64.Vec3
		hurt bool
	}{{1, mgl64.Vec3{0.1, 0, 0}, true}, {3, mgl64.Vec3{0, 0, -0.05}, true}, {0, mgl64.Vec3{0.1, 0, 0}, false}, {3, mgl64.Vec3{0, -0.08, 0}, false}} {
		m := &berryMob{vel: c.vel}
		block.SweetBerryBush{Age: c.age}.EntityInside(cube.Pos{}, nil, m)
		if (m.hurt > 0) != c.hurt || (c.hurt && m.hurt != 1) {
			t.Errorf("age %d vel %v: hurt %v", c.age, c.vel, m.hurt)
		}
		if c.hurt {
			if src, ok := m.src.(block.DamageSource); !ok || src.Block != (block.SweetBerryBush{Age: c.age}) {
				t.Errorf("damage source %#v", m.src)
			}
		}
		if want := (mgl64.Vec3{c.vel[0] * 0.8, c.vel[1] * 0.75, c.vel[2] * 0.8}); !m.vel.ApproxEqual(want) {
			t.Errorf("velocity %v, want %v", m.vel, want)
		}
	}
}
