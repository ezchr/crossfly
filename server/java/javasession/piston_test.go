//go:build crossfly_full

package javasession

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	jchunk "github.com/df-mc/dragonfly/server/java/protocol/chunk"
	"github.com/df-mc/dragonfly/server/java/protocol/version"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
	"testing"
)

func TestMovingPistonJavaBlockEntity(t *testing.T) {
	moving := block.MovingPiston{Moving: block.Stone{}, Facing: cube.FaceEast, Extending: true, LastProgress: .5}
	typ, network := javaBlockEntityData(moving)
	if typ != "minecraft:piston" || len(network) < 2 || network[0] != 10 {
		t.Fatalf("missing moving piston NBT: %s", typ)
	}
	disk := append([]byte{network[0], 0, 0}, network[1:]...)
	var data map[string]any
	if err := nbt.UnmarshalEncoding(disk, &data, nbt.BigEndian); err != nil {
		t.Fatal(err)
	}
	state, ok := data["blockState"].(map[string]any)
	if !ok || state["Name"] != "minecraft:stone" {
		t.Fatalf("wrong carried state: %v", state)
	}
	if data["facing"] != int32(5) || data["extending"] != byte(1) || data["source"] != byte(0) || data["progress"] != float32(.5) {
		t.Fatalf("wrong animation data: %v", data)
	}
}

func TestPistonChunkUsesDynamicExtendedState(t *testing.T) {
	s := &Session{ver: version.Newest, dim: "minecraft:overworld"}
	col := &jchunk.Column{Sections: make([]jchunk.Section, 24)}
	pos := cube.Pos{3, 64, 7}
	p := block.Piston{Facing: cube.FaceNorth, Extended: true, State: 2}
	s.addBlockEntities(col, map[cube.Pos]world.Block{pos: p})
	state := col.Sections[8].Blocks[jchunk.BlockIndex(3, 0, 7)]
	if state != uint32(p.JavaState()) {
		t.Fatalf("extended piston chunk state %d, want %d", state, p.JavaState())
	}
	if len(col.BlockEntities) != 0 {
		t.Fatal("stationary Java piston must not have a block entity")
	}
	moving := block.MovingPiston{Moving: block.PistonHead{Facing: cube.FaceNorth, Sticky: true}, Facing: cube.FaceNorth, Extending: true, Source: true}
	s.addBlockEntities(col, map[cube.Pos]world.Block{pos: moving})
	if len(col.BlockEntities) != 1 || col.BlockEntities[0].Type != version.Newest.BuiltinID("minecraft:block_entity_type", "minecraft:piston") {
		t.Fatal("chunk omitted moving piston entity")
	}
	if col.Sections[8].Blocks[jchunk.BlockIndex(3, 0, 7)] != uint32(moving.JavaState()) {
		t.Fatal("chunk omitted moving piston state")
	}
}
