//go:build crossfly_full

package mcdb

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/goleveldb/leveldb"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
	"testing"
)

func TestScheduledTickPriorityPersistenceAndLegacyDefaults(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	key := dbKey{pos: world.ChunkPos{0, 0}, dim: world.Overworld}
	want := chunk.ScheduledBlockUpdate{Pos: cube.Pos{1, 64, 2}, Block: world.BlockRuntimeID(block.RedstoneRepeater{Facing: cube.North}), Tick: 105, Priority: -2, SubTickOrder: 777}
	batch := new(leveldb.Batch)
	db.storeScheduledUpdates(batch, key, 100, []chunk.ScheduledBlockUpdate{want})
	if err := db.ldb.Write(batch, nil); err != nil {
		t.Fatal(err)
	}
	got, tick, err := db.scheduledUpdates(key)
	if err != nil || tick != 100 || len(got) != 1 || got[0] != want {
		t.Fatalf("saved tick roundtrip: %v tick%d err%v", got, tick, err)
	}
	// Bedrock and older fork saves do not carry the optional priority/order extension.
	palette := chunk.BlockPaletteEncoding{Blocks: db.conf.Blocks}
	data, err := nbt.MarshalEncoding(scheduledUpdates{CurrentTick: 100, TickList: []map[string]any{{"x": int32(1), "y": int32(64), "z": int32(2), "time": int64(105), "blockState": palette.EncodeBlockState(want.Block)}}}, nbt.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ldb.Put(key.Sum(keyPendingScheduledTicks), data, nil); err != nil {
		t.Fatal(err)
	}
	got, _, err = db.scheduledUpdates(key)
	if err != nil || len(got) != 1 || got[0].Priority != 0 || got[0].SubTickOrder != 0 {
		t.Fatalf("legacy tick defaults: %v err%v", got, err)
	}
}
