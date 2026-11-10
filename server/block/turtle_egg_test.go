//go:build crossfly_full

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
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The egg blocks only register with the mobs.
func init() {
	block.RegisterMobBlocks()
	item.RegisterMobItems()
}

// eggUser is a user holding an item that places blocks straight into tx.
type eggUser struct {
	held item.Stack
	tx   *world.Tx
}

func (u *eggUser) Close() error                        { return nil }
func (u *eggUser) H() *world.EntityHandle              { return nil }
func (u *eggUser) Position() mgl64.Vec3                { return mgl64.Vec3{} }
func (u *eggUser) Rotation() cube.Rotation             { return cube.Rotation{} }
func (u *eggUser) HeldItems() (item.Stack, item.Stack) { return u.held, item.Stack{} }
func (u *eggUser) SetHeldItems(m, _ item.Stack)        { u.held = m }
func (u *eggUser) UsingItem() bool                     { return false }
func (u *eggUser) ReleaseItem()                        {}
func (u *eggUser) UseItem()                            {}
func (u *eggUser) PlaceBlock(pos cube.Pos, b world.Block, ctx *item.UseContext) {
	u.tx.SetBlock(pos, b, nil)
	ctx.CountSub++
}

// eggWorld returns a synchronous world closed at the end of the test.
func eggWorld(t *testing.T) *world.World {
	t.Helper()
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

// eggDo runs f in a transaction of w.
func eggDo(t *testing.T, w *world.World, f func(tx *world.Tx)) {
	t.Helper()
	if err := w.Do(f).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

var eggCreativeOnce sync.Once

// eggInCreative reports whether an item of the type of it is in the creative inventory.
func eggInCreative(it world.Item) bool {
	eggCreativeOnce.Do(eggCreativeRegister)
	for _, c := range creative.Items() {
		if c.Stack.Item() == it {
			return true
		}
	}
	return false
}

//go:linkname eggCreativeRegister github.com/df-mc/dragonfly/server/item/creative.registerCreativeItems
func eggCreativeRegister()

// eggHatchWindow is a world time whose time of day (0.676) is in the 0.65-0.69 window turtle eggs move in.
const eggHatchWindow = 21600

// TestTurtleEggStates checks that every turtle egg state is a Bedrock block state of the vanilla palette, that
// the block is an item, and that the item is in the creative inventory.
func TestTurtleEggStates(t *testing.T) {
	eggWorld(t)
	counts := []string{"one_egg", "two_egg", "three_egg", "four_egg"}
	cracks := []string{"no_cracks", "cracked", "max_cracked"}
	for c, cn := range counts {
		for cr, crn := range cracks {
			b, ok := world.BlockByName("minecraft:turtle_egg", map[string]any{"turtle_egg_count": cn, "cracked_state": crn})
			if want := (block.TurtleEgg{AdditionalCount: c, Cracks: cr}); !ok || b != want {
				t.Errorf("%v %v: got %v (ok=%v), want %v", cn, crn, b, ok, want)
			}
		}
	}
	if it, ok := world.ItemByName("minecraft:turtle_egg", 0); !ok || it != (block.TurtleEgg{}) {
		t.Errorf("turtle egg item: %v %v", it, ok)
	}
	if !eggInCreative(block.TurtleEgg{}) {
		t.Error("the turtle egg is not in the creative inventory")
	}
}

// TestTurtleEggPlacing: a turtle egg goes on a block that supports it but not on magma; using one on turtle
// eggs adds an egg, up to four, and queues their tick.
func TestTurtleEggPlacing(t *testing.T) {
	w := eggWorld(t)
	sand, pos := cube.Pos{0, 0, 0}, cube.Pos{0, 1, 0}
	eggDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(sand, block.Sand{}, nil)
		tx.SetBlock(cube.Pos{2, 0, 0}, block.Magma{}, nil)
		u := &eggUser{held: item.NewStack(block.TurtleEgg{}, 8), tx: tx}
		use := func(p cube.Pos) bool {
			return block.TurtleEgg{}.UseOnBlock(p, cube.FaceUp, mgl64.Vec3{}, tx, u, &item.UseContext{})
		}
		if use(cube.Pos{2, 0, 0}) {
			t.Error("a turtle egg went on magma")
		}
		if !use(sand) || tx.Block(pos) != (block.TurtleEgg{}) {
			t.Fatalf("placing on sand: %v", tx.Block(pos))
		}
		if !tx.BlockUpdateScheduled(pos, block.TurtleEgg{}) {
			t.Error("no tick queued for the placed egg")
		}
		for c := 1; c <= 3; c++ {
			if !use(pos) || tx.Block(pos) != (block.TurtleEgg{AdditionalCount: c}) {
				t.Fatalf("adding egg %d: %v", c+1, tx.Block(pos))
			}
		}
		if !tx.BlockUpdateScheduled(pos, block.TurtleEgg{AdditionalCount: 3}) {
			t.Error("no tick queued for four eggs")
		}
		if use(pos) {
			t.Error("a fifth egg went on four")
		}
	})
}

// TestTurtleEggHatching: on sand, in the late night window, turtle eggs crack twice and then hatch (without
// mobs only the block goes); outside it and off sand they stay as they are.
func TestTurtleEggHatching(t *testing.T) {
	w := eggWorld(t)
	w.StopTime()
	pos, red := cube.Pos{0, 1, 0}, cube.Pos{3, 1, 0}
	r := rand.New(rand.NewPCG(3, 4))
	eggDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(pos.Side(cube.FaceDown), block.Sand{}, nil)
		tx.SetBlock(red.Side(cube.FaceDown), block.Sand{Red: true}, nil)
		tx.SetBlock(pos, block.TurtleEgg{AdditionalCount: 1}, nil)
		tx.SetBlock(red, block.TurtleEgg{}, nil)
	})
	w.SetTime(eggNoon)
	eggDo(t, w, func(tx *world.Tx) {
		cracked := 0
		for i := 0; i < 100; i++ {
			e := block.TurtleEgg{AdditionalCount: 1}
			tx.SetBlock(pos, e, nil)
			e.ScheduledTick(pos, tx, r)
			if tx.Block(pos) != e {
				cracked++
			}
		}
		if cracked > 3 {
			t.Errorf("at noon turtle eggs cracked %d times in 100 steps, want about 1 in 500", cracked)
		}
		tx.SetBlock(pos, block.TurtleEgg{AdditionalCount: 1}, nil)
	})
	w.SetTime(eggHatchWindow)
	eggDo(t, w, func(tx *world.Tx) {
		for cr := 1; cr <= 2; cr++ {
			tx.Block(pos).(block.TurtleEgg).ScheduledTick(pos, tx, r)
			if want := (block.TurtleEgg{AdditionalCount: 1, Cracks: cr}); tx.Block(pos) != want {
				t.Fatalf("step %d: %v, want %v", cr, tx.Block(pos), want)
			}
			if !tx.BlockUpdateScheduled(pos, tx.Block(pos)) {
				t.Errorf("step %d: no next tick queued", cr)
			}
		}
		tx.Block(pos).(block.TurtleEgg).ScheduledTick(pos, tx, r)
		if _, ok := tx.Block(pos).(block.Air); !ok {
			t.Errorf("max cracked eggs left %v after hatching", tx.Block(pos))
		}
		block.TurtleEgg{}.ScheduledTick(red, tx, r)
		if tx.Block(red) != (block.TurtleEgg{}) {
			t.Errorf("turtle egg on red sand cracked: %v", tx.Block(red))
		}
	})
}

// eggNoon is the world time of noon.
const eggNoon = 6000

// TestTurtleEggQueuedTicks: placed turtle eggs take their first step 1000 ticks later; eggs set without a tick
// (by a command) get one from a random tick.
func TestTurtleEggQueuedTicks(t *testing.T) {
	w := eggWorld(t)
	w.StopTime()
	w.SetTime(eggHatchWindow)
	pos, set := cube.Pos{0, 1, 0}, cube.Pos{4, 1, 0}
	eggDo(t, w, func(tx *world.Tx) {
		for x := 0; x <= 4; x++ {
			tx.SetBlock(cube.Pos{x, -1, 0}, block.Stone{}, nil) // Sand falls.
			tx.SetBlock(cube.Pos{x, 0, 0}, block.Sand{}, nil)
		}
		block.TurtleEgg{}.UseOnBlock(pos.Side(cube.FaceDown), cube.FaceUp, mgl64.Vec3{}, tx, &eggUser{held: item.NewStack(block.TurtleEgg{}, 1), tx: tx}, &item.UseContext{})
		tx.SetBlock(set, block.TurtleEgg{}, nil)
		if tx.BlockUpdateScheduled(set, block.TurtleEgg{}) {
			t.Fatal("an egg set by SetBlock has a tick")
		}
		block.TurtleEgg{}.RandomTick(set, tx, rand.New(rand.NewPCG(1, 1)))
		if !tx.BlockUpdateScheduled(set, block.TurtleEgg{}) {
			t.Error("a random tick did not queue the egg's tick")
		}
	})
	for i := 0; i < 999; i++ {
		w.AdvanceTick()
	}
	eggDo(t, w, func(tx *world.Tx) {
		if tx.Block(pos) != (block.TurtleEgg{}) {
			t.Fatalf("the egg moved before 1000 ticks: %v", tx.Block(pos))
		}
	})
	w.AdvanceTick()
	eggDo(t, w, func(tx *world.Tx) {
		if tx.Block(pos) != (block.TurtleEgg{Cracks: 1}) {
			t.Errorf("after 1000 ticks in the hatch window: %v, want cracked", tx.Block(pos))
		}
	})
}

// TestTurtleEggBreaking: a player breaks one egg at a time; only silk touch drops it.
func TestTurtleEggBreaking(t *testing.T) {
	w := eggWorld(t)
	pos := cube.Pos{0, 1, 0}
	silk := item.NewStack(item.Pickaxe{Tier: item.ToolTierIron}, 1).WithEnchantments(item.NewEnchantment(enchantment.SilkTouch, 1))
	info := block.TurtleEgg{AdditionalCount: 2}.BreakInfo()
	if d := info.Drops(item.ToolNone{}, nil); len(d) != 0 {
		t.Errorf("turtle eggs dropped %v without silk touch", d)
	}
	if d := info.Drops(item.ToolNone{}, silk.Enchantments()); len(d) != 1 || d[0].Count() != 1 || d[0].Item() != (block.TurtleEgg{}) {
		t.Errorf("silk touch drops %v, want one turtle egg", d)
	}
	eggDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(pos, block.TurtleEgg{AdditionalCount: 2, Cracks: 1}, nil)
		for want := 1; want >= 0; want-- {
			b := tx.Block(pos).(block.TurtleEgg)
			tx.SetBlock(pos, nil, nil)
			b.BreakInfo().BreakHandler(pos, tx, &eggUser{held: silk, tx: tx})
			if got := tx.Block(pos); got != (block.TurtleEgg{AdditionalCount: want, Cracks: 1}) {
				t.Fatalf("after a break: %v, want %d additional eggs", got, want)
			}
		}
		tx.SetBlock(pos, nil, nil)
		block.TurtleEgg{Cracks: 1}.BreakInfo().BreakHandler(pos, tx, &eggUser{tx: tx})
		if _, ok := tx.Block(pos).(block.Air); !ok {
			t.Errorf("breaking the last egg left %v", tx.Block(pos))
		}
	})
}
