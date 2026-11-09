//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"testing"
)

type trappedChestViewer struct {
	world.Viewer
	spectator bool
}

func (*trappedChestViewer) ViewSlotChange(int, item.Stack)           {}
func (v *trappedChestViewer) CountsForContainerPower(*world.Tx) bool { return !v.spectator }

func TestTrappedChestOpenersAndSpectators(t *testing.T) {
	w := world.Config{Synchronous: true, SettleSynchronousTransactions: true}.New()
	defer w.Close()
	pos := cube.Pos{0, 64, 0}
	viewers := make([]*trappedChestViewer, 17)
	runWorld(w, func(tx *world.Tx) {
		tx.SetBlock(pos, NewTrappedChest(), nil)
		tx.SetBlock(pos.Side(cube.FaceDown), Stone{}, nil)
		tx.SetBlock(pos.Side(cube.FaceEast), RedstoneLamp{}, nil)
		for i := range viewers {
			viewers[i] = &trappedChestViewer{}
			tx.Block(pos).(Chest).AddViewer(viewers[i], tx, pos)
		}
		c := tx.Block(pos).(Chest)
		if c.signal != 15 {
			t.Fatalf("17 viewers: power=%d", c.signal)
		}
		for _, face := range cube.Faces() {
			if c.RedstonePower(pos, tx, face) != 15 {
				t.Errorf("no weak power on %v", face)
			}
			strong := 0
			if face == cube.FaceDown {
				strong = 15
			}
			if c.RedstoneStrongPower(pos, tx, face) != strong {
				t.Errorf("strong power on %v", face)
			}
		}
	})
	runWorld(w, func(tx *world.Tx) {
		if !tx.Block(pos.Side(cube.FaceEast)).(RedstoneLamp).Lit {
			t.Error("opening did not power neighbouring lamp")
		}
	})
	runWorld(w, func(tx *world.Tx) {
		for _, v := range viewers[1:] {
			tx.Block(pos).(Chest).RemoveViewer(v, tx, pos)
		}
		if tx.Block(pos).(Chest).signal != 1 {
			t.Fatal("single opener did not give power 1")
		}
		viewers[0].spectator = true
		tx.Block(pos).(Chest).Tick(5, pos, tx)
		if tx.Block(pos).(Chest).signal != 0 {
			t.Fatal("spectator still counted")
		}
		viewers[0].spectator = false
		tx.Block(pos).(Chest).Tick(10, pos, tx)
		if tx.Block(pos).(Chest).signal != 1 {
			t.Fatal("returning player did not count")
		}
		tx.Block(pos).(Chest).RemoveViewer(viewers[0], tx, pos)
		if tx.Block(pos).(Chest).signal != 0 {
			t.Fatal("closed chest stayed powered")
		}
	})
}

func TestTrappedChestPairingAndPersistence(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	at := cube.Pos{0, 64, 0}
	other := at.Side(cube.FaceEast)
	runWorld(w, func(tx *world.Tx) {
		c := NewTrappedChest()
		c.Facing = cube.North
		ordinary := NewChest()
		ordinary.Facing = cube.North
		tx.SetBlock(at, c, nil)
		tx.SetBlock(other, ordinary, nil)
		if _, _, ok := c.pair(tx, at, other); ok {
			t.Fatal("trapped and ordinary chests paired")
		}
		tx.SetBlock(other, c, nil)
		a, b, ok := c.pair(tx, at, other)
		if !ok {
			t.Fatal("trapped pair rejected")
		}
		tx.SetBlock(at, a, nil)
		tx.SetBlock(other, b, nil)
		v := &trappedChestViewer{}
		tx.Block(at).(Chest).AddViewer(v, tx, at)
		if tx.Block(at).(Chest).signal != 1 || tx.Block(other).(Chest).signal != 1 {
			t.Fatal("both halves must power")
		}
		tx.Block(at).(Chest).RemoveViewer(v, tx, at)
		if tx.Block(other).(Chest).signal != 0 {
			t.Fatal("paired half stayed powered")
		}
		decoded := c.DecodeNBT(c.EncodeNBT()).(Chest)
		name, _ := decoded.EncodeItem()
		blockName, _ := decoded.EncodeBlock()
		if !decoded.Trapped || name != "minecraft:trapped_chest" || blockName != name {
			t.Fatal("trapped variant lost during persistence")
		}
		normal := NewChest()
		tx.SetBlock(other, normal, nil)
		normal.AddViewer(v, tx, other)
		if tx.Block(other).(Chest).RedstonePower(other, tx, cube.FaceEast) != 0 {
			t.Fatal("ordinary chest powers redstone")
		}
	})
}
