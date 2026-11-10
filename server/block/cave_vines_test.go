package block_test

import (
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestCaveVinesStates checks that the 78 cave vine states are Bedrock states, that glow berries are the item of
// the vines and in the creative inventory, and that vines with berries give light 14.
func TestCaveVinesStates(t *testing.T) {
	berryWorld(t)
	for age := 0; age <= 25; age++ {
		for name, want := range map[string]block.CaveVines{
			"minecraft:cave_vines":                   {Age: age},
			"minecraft:cave_vines_head_with_berries": {Age: age, Berries: true},
			"minecraft:cave_vines_body_with_berries": {Age: age, Berries: true, Body: true},
		} {
			b, ok := world.BlockByName(name, map[string]any{"growing_plant_age": int32(age)})
			if !ok || b != want {
				t.Errorf("%s %d: got %v (ok=%v), want %v", name, age, b, ok, want)
			}
		}
	}
	if it, ok := world.ItemByName("minecraft:glow_berries", 0); !ok || it != (block.CaveVines{}) {
		t.Errorf("glow berries item: %v %v", it, ok)
	}
	if !berryInCreative(block.CaveVines{}) {
		t.Error("glow berries are not in the creative inventory")
	}
	if l := (block.CaveVines{Berries: true}).LightEmissionLevel(); l != 14 {
		t.Errorf("light with berries %d, want 14", l)
	}
	if l := (block.CaveVines{}).LightEmissionLevel(); l != 0 {
		t.Errorf("light without berries %d, want 0", l)
	}
}

// TestCaveVinesPlacing: glow berries plant a vine under the bottom face of a solid block or of a vine tip, not on
// top of a block; a vine breaks when its support goes, and the vines below it with it.
func TestCaveVinesPlacing(t *testing.T) {
	w := berryWorld(t)
	ceiling := cube.Pos{0, 10, 0}
	berryDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(ceiling, block.Stone{}, nil)
		u := &berryUser{held: item.NewStack(block.CaveVines{}, 8), tx: tx}
		use := func(p cube.Pos, f cube.Face) bool {
			return block.CaveVines{}.UseOnBlock(p, f, mgl64.Vec3{}, tx, u, &item.UseContext{})
		}
		if use(ceiling, cube.FaceUp) {
			t.Error("glow berries planted on top of stone")
		}
		if !use(ceiling, cube.FaceDown) {
			t.Fatal("no vine under the stone")
		}
		if _, ok := tx.Block(ceiling.Side(cube.FaceDown)).(block.CaveVines); !ok {
			t.Fatalf("under the stone: %v", tx.Block(ceiling.Side(cube.FaceDown)))
		}
		if !use(cube.Pos{0, 9, 0}, cube.FaceDown) {
			t.Fatal("no vine under the vine")
		}
		tx.SetBlock(ceiling, nil, nil)
		tx.Block(cube.Pos{0, 9, 0}).(block.CaveVines).NeighbourUpdateTick(cube.Pos{0, 9, 0}, ceiling, tx)
		tx.Block(cube.Pos{0, 8, 0}).(block.CaveVines).NeighbourUpdateTick(cube.Pos{0, 8, 0}, cube.Pos{0, 9, 0}, tx)
		for _, p := range []cube.Pos{{0, 9, 0}, {0, 8, 0}} {
			if _, ok := tx.Block(p).(block.Air); !ok {
				t.Errorf("vine at %v stayed without support: %v", p, tx.Block(p))
			}
		}
	})
}

// TestCaveVinesGrowth: a vine tip grows downwards on random ticks, older by one, some with berries; a berry vine
// that gets vine below turns into the body form; bone meal adds berries.
func TestCaveVinesGrowth(t *testing.T) {
	w := berryWorld(t)
	top := cube.Pos{0, 30, 0}
	berryDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(top.Side(cube.FaceUp), block.Stone{}, nil)
		tx.SetBlock(top, block.CaveVines{Age: 0}, nil)
		r := rand.New(rand.NewPCG(5, 6))
		tip := top
		for i := 0; i < 4000 && tip[1] > 10; i++ {
			for p := top; p[1] >= tip[1]; p = p.Side(cube.FaceDown) {
				if v, ok := tx.Block(p).(block.CaveVines); ok {
					v.RandomTick(p, tx, r)
				}
			}
			if _, ok := tx.Block(tip.Side(cube.FaceDown)).(block.CaveVines); ok {
				tip = tip.Side(cube.FaceDown)
			}
		}
		if tip[1] > 10 {
			t.Fatalf("the vine grew only to y %d", tip[1])
		}
		berries := 0
		for p, age := top, 0; p[1] >= tip[1]; p, age = p.Side(cube.FaceDown), age+1 {
			v := tx.Block(p).(block.CaveVines)
			if v.Age != age {
				t.Fatalf("vine at %v age %d, want %d", p, v.Age, age)
			}
			if v.Berries {
				berries++
				if v.Body != (p != tip) {
					t.Errorf("berry vine at %v: body %v", p, v.Body)
				}
			}
		}
		if berries == 0 {
			t.Error("no vine grew berries")
		}
		mid := cube.Pos{0, 20, 0}
		tx.SetBlock(mid, block.CaveVines{Age: 10}, nil)
		if res := (block.CaveVines{Age: 10}).BoneMeal(mid, tx); res != item.BoneMealResultSmall || tx.Block(mid) != (block.CaveVines{Age: 10, Berries: true, Body: true}) {
			t.Errorf("bone meal: %v, %v", res, tx.Block(mid))
		}
		if res := (block.CaveVines{Age: 10, Berries: true}).BoneMeal(mid, tx); res != item.BoneMealResultNone {
			t.Errorf("bone meal on berries: %v", res)
		}
	})
}

// TestCaveVinesPicking: using a vine with berries drops one glow berry and leaves a vine without berries;
// breaking drops the berry, nothing without one; raiding picks.
func TestCaveVinesPicking(t *testing.T) {
	w := berryWorld(t)
	pos := cube.Pos{0, 9, 0}
	berryDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(pos.Side(cube.FaceUp), block.Stone{}, nil)
		v := block.CaveVines{Age: 4, Berries: true}
		tx.SetBlock(pos, v, nil)
		if !v.Activate(pos, cube.FaceDown, tx, &berryUser{tx: tx}, &item.UseContext{}) {
			t.Fatal("picking failed")
		}
		if got, ok := tx.Block(pos).(block.CaveVines); !ok || got.Berries {
			t.Errorf("after picking: %v", tx.Block(pos))
		}
		if n := berryDrops(tx, block.CaveVines{}); n != 1 {
			t.Errorf("picked %d glow berries, want 1", n)
		}
		if (block.CaveVines{}).Activate(pos, cube.FaceDown, tx, &berryUser{tx: tx}, &item.UseContext{}) {
			t.Error("picked a vine without berries")
		}
		tx.SetBlock(pos, block.CaveVines{Age: 4, Berries: true, Body: true}, nil)
		block.CaveVines{Age: 4, Berries: true, Body: true}.Raid(pos, tx, nil)
		if got, ok := tx.Block(pos).(block.CaveVines); !ok || got.Berries || berryDrops(tx, block.CaveVines{}) != 1 {
			t.Errorf("raiding: %v", tx.Block(pos))
		}
	})
	if d := (block.CaveVines{Berries: true}).BreakInfo().Drops(item.ToolNone{}, nil); len(d) != 1 || d[0].Count() != 1 {
		t.Errorf("berry vine drops %v", d)
	}
	if d := (block.CaveVines{}).BreakInfo().Drops(item.ToolNone{}, nil); len(d) != 0 {
		t.Errorf("plain vine drops %v", d)
	}
	if !(block.CaveVines{Berries: true}).Raidable() || (block.CaveVines{}).Raidable() {
		t.Error("raidable only with berries")
	}
}
