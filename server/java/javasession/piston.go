//go:build crossfly_full

package javasession

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/java/javamap"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

type javaMovingBlock interface {
	JavaMovingBlock() (world.Block, cube.Face, bool, bool, float64)
}

func javaBlockEntityData(jb JavaBlock) (string, []byte) {
	moving, ok := jb.(javaMovingBlock)
	if !ok {
		return jb.JavaBlockEntity()
	}
	b, face, extending, source, progress := moving.JavaMovingBlock()
	if b == nil {
		return "", nil
	}
	id := int32(0)
	if js, ok := b.(interface{ JavaState() int32 }); ok {
		id = js.JavaState()
	} else {
		id = javamap.DefaultBlockStates()[world.BlockRuntimeID(b)]
	}
	state, ok := javamap.BlockStateData(id)
	if !ok {
		return "", nil
	}
	bit := func(b bool) byte {
		if b {
			return 1
		}
		return 0
	}
	data, err := nbt.MarshalEncoding(map[string]any{"blockState": state, "facing": int32(face), "extending": bit(extending), "source": bit(source), "progress": float32(progress)}, nbt.BigEndian)
	if err != nil || len(data) < 3 {
		return "", nil
	}
	return "minecraft:piston", append([]byte{data[0]}, data[3:]...)
}
