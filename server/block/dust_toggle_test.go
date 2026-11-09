//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"testing"
)

func TestDustDotToggleChangesPowerAndConnectsAgain(t *testing.T) {
	w := world.Config{Synchronous: true, SettleSynchronousTransactions: true}.New()
	defer w.Close()
	at := cube.Pos{0, 64, 0}
	runWorld(w, func(tx *world.Tx) {
		tx.SetBlock(at.Side(cube.FaceDown), Stone{}, nil)
		tx.SetBlock(at.Side(cube.FaceUp), RedstoneBlock{}, nil)
		tx.SetBlock(at.Side(cube.FaceEast), RedstoneLamp{}, nil)
		tx.SetBlock(at, RedstoneWire{}, nil)
	})
	runWorld(w, func(tx *world.Tx) {
		r := tx.Block(at).(RedstoneWire)
		if r.Power != 15 || r.Connections.Uint8() != 40 {
			t.Fatalf("initial cross: %+v", r)
		}
		if !r.Activate(at, cube.FaceUp, tx, nil, &item.UseContext{}) {
			t.Fatal("cross could not become dot")
		}
	})
	runWorld(w, func(tx *world.Tx) {
		r := tx.Block(at).(RedstoneWire)
		if !r.Dot || r.Power != 15 || r.Connections.Uint8() != 0 {
			t.Fatalf("dot: %+v", r)
		}
		if r.RedstonePower(at, tx, cube.FaceEast) != 0 || r.RedstonePower(at, tx, cube.FaceDown) != 15 {
			t.Fatal("dot must power only downward")
		}
		decoded := r.DecodeNBT(r.EncodeNBT()).(RedstoneWire)
		if !decoded.Dot {
			t.Fatal("dot lost in saved NBT")
		}
		if r.EncodeNetworkNBT() != nil {
			t.Fatal("fake dust block actor sent to Bedrock")
		}
		if !r.Activate(at, cube.FaceUp, tx, nil, &item.UseContext{}) {
			t.Fatal("dot could not become cross")
		}
	})
	runWorld(w, func(tx *world.Tx) {
		r := tx.Block(at).(RedstoneWire)
		if r.Dot || r.Connections.Uint8() != 40 {
			t.Fatal("cross not restored")
		}
		r.Activate(at, cube.FaceUp, tx, nil, &item.UseContext{})
		beside := at.Side(cube.FaceNorth)
		tx.SetBlock(beside.Side(cube.FaceDown), Stone{}, nil)
		tx.SetBlock(beside, RedstoneWire{}, nil)
	})
	runWorld(w, func(tx *world.Tx) {
		r := tx.Block(at).(RedstoneWire)
		if r.Dot || r.Connections.Uint8() == 0 {
			t.Fatal("connecting neighbour did not turn dot into line")
		}
		if r.Activate(at, cube.FaceUp, tx, nil, &item.UseContext{}) {
			t.Fatal("line allowed dot toggle")
		}
		tx.SetBlock(at.Side(cube.FaceNorth), Air{}, nil)
	})
	runWorld(w, func(tx *world.Tx) {
		r := tx.Block(at).(RedstoneWire)
		if r.Dot || r.Connections.Uint8() != 40 {
			t.Fatal("removing neighbour must restore cross")
		}
	})
}
