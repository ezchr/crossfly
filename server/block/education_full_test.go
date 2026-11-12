//go:build crossfly_full

package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func TestEducationBlocksRegistered(t *testing.T) {
	_ = world.Config{Synchronous: true}.New().Close() // finalises the block registry
	for _, name := range []string{"minecraft:allow", "minecraft:deny"} {
		if _, ok := world.BlockByName(name, nil); !ok {
			t.Errorf("%s not registered", name)
		}
		if _, ok := world.ItemByName(name, 0); !ok {
			t.Errorf("%s item not registered", name)
		}
	}
	if n := len(allBorderBlocks()); n != 162 {
		t.Fatalf("border block states: %d, want 162", n)
	}
	b, ok := world.BlockByName("minecraft:border_block", map[string]any{
		"wall_connection_type_north": "tall", "wall_connection_type_east": "none",
		"wall_connection_type_south": "short", "wall_connection_type_west": "none", "wall_post_bit": uint8(1),
	})
	if !ok {
		t.Fatal("border block state not registered")
	}
	if bb := b.(BorderBlock); bb.NorthConnection != TallWallConnection() || bb.SouthConnection != ShortWallConnection() || !bb.Post {
		t.Fatalf("decoded %+v", bb)
	}
	if _, ok := world.ItemByName("minecraft:border_block", 0); !ok {
		t.Error("border block item not registered")
	}
	for _, b := range []world.Block{Allow{}, Deny{}, BorderBlock{}} {
		if _, ok := b.(Breakable); ok {
			t.Errorf("%T is breakable in survival", b)
		}
	}
}

func TestBuildPermittedColumnScan(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	runWorld(w, func(tx *world.Tx) {
		r := tx.Range()
		// No marker: the default.
		if !tx.BuildPermitted(cube.Pos{0, 70, 0}, true) || tx.BuildPermitted(cube.Pos{0, 70, 0}, false) {
			t.Error("no marker: want the default")
		}
		// Above the top: never.
		if tx.BuildPermitted(cube.Pos{0, r[1] + 1, 0}, true) {
			t.Error("above the top of the world was permitted")
		}

		// Deny at 60: forbidden above it and at it, default below it. Nothing is looked up above.
		tx.SetBlock(cube.Pos{0, 60, 0}, Deny{}, nil)
		for _, y := range []int{60, 61, 100, r[1]} {
			if tx.BuildPermitted(cube.Pos{0, y, 0}, true) {
				t.Errorf("deny below y %d: permitted", y)
			}
		}
		if !tx.BuildPermitted(cube.Pos{0, 59, 0}, true) || tx.BuildPermitted(cube.Pos{0, 59, 0}, false) {
			t.Error("below the deny block: want the default")
		}
		// The next column is free.
		if !tx.BuildPermitted(cube.Pos{1, 70, 0}, true) {
			t.Error("deny leaked into the next column")
		}

		// Allow above the deny: the first marker found going down decides.
		tx.SetBlock(cube.Pos{0, 80, 0}, Allow{}, nil)
		if !tx.BuildPermitted(cube.Pos{0, 90, 0}, false) || !tx.BuildPermitted(cube.Pos{0, 80, 0}, false) {
			t.Error("allow above deny: forbidden")
		}
		if tx.BuildPermitted(cube.Pos{0, 79, 0}, true) {
			t.Error("between deny (below) and allow (above): permitted")
		}
		// Allow lets an otherwise refused default build, in another sub chunk far above.
		tx.SetBlock(cube.Pos{5, r[0], 5}, Allow{}, nil)
		if !tx.BuildPermitted(cube.Pos{5, r[1], 5}, false) {
			t.Error("allow at the bottom: forbidden at the top")
		}
	})
}

func TestBorderColumn(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	runWorld(w, func(tx *world.Tx) {
		r := tx.Range()
		tx.SetBlock(cube.Pos{3, 10, 7}, BorderBlock{Post: true}, nil)
		tx.SetBlock(cube.Pos{3, 200, 6}, Allow{}, nil)
		if !tx.HasBorderBlock(3, 7) || tx.HasBorderBlock(3, 6) || tx.HasBorderBlock(4, 7) {
			t.Fatal("border column lookup wrong")
		}
		for _, y := range []int{r[0], 9, 10, 11, r[1]} {
			if tx.BuildPermitted(cube.Pos{3, y, 7}, true) {
				t.Errorf("border column y %d: permitted", y)
			}
		}
		// Even an allow block in the column does not help.
		tx.SetBlock(cube.Pos{3, 5, 7}, Allow{}, nil)
		if tx.BuildPermitted(cube.Pos{3, 6, 7}, true) {
			t.Error("allow inside a border column: permitted")
		}
	})
}

func TestBorderColumnsPayload(t *testing.T) {
	_ = world.Config{Synchronous: true}.New().Close() // finalises the block registry
	c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
	c.SetBlock(2, 0, 1, 0, world.BlockRuntimeID(BorderBlock{}))
	c.SetBlock(15, 100, 15, 0, world.BlockRuntimeID(BorderBlock{Post: true}))
	c.SetBlock(4, 50, 4, 0, world.BlockRuntimeID(Allow{}))
	got := world.BorderColumns(c)
	want := []byte{2 | 1<<4, 15 | 15<<4}
	if string(got) != string(want) {
		t.Fatalf("border columns %v, want %v", got, want)
	}
	if cols := world.BorderColumns(chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())); cols != nil {
		t.Fatalf("empty chunk: %v", cols)
	}
}

func TestWallConnectsToBorderBlock(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	runWorld(w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 64, 0}, BorderBlock{Post: true}, nil)
		wall, _ := Wall{Block: Cobblestone{}}.calculateConnections(tx, cube.Pos{1, 64, 0})
		if wall.WestConnection == NoWallConnection() {
			t.Error("wall does not connect to a border block")
		}
		bb, _ := BorderBlock{}.wall().calculateConnections(tx, cube.Pos{0, 64, 1})
		if bb.NorthConnection == NoWallConnection() {
			t.Error("border block does not connect to a border block")
		}
	})
}
