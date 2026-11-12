package session

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"testing"
)

func TestBlockPreviewPrivateUpdatesAndChunkReplay(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	s := &Session{br: world.DefaultBlockRegistry, packets: make(chan packet.Packet, 8)}
	other := &Session{br: world.DefaultBlockRegistry, packets: make(chan packet.Packet, 8)}
	pos := cube.Pos{1, 5, 1}
	s.SetBlockPreviews(map[cube.Pos]world.Block{pos: block.Glass{}})
	s.ViewBlockUpdate(pos, block.Air{}, 0)
	pk := (<-s.packets).(*packet.UpdateBlock)
	if pk.NewBlockRuntimeID != world.BlockRuntimeID(block.Glass{}) {
		t.Error("real update erased preview")
	}
	other.ViewBlockUpdate(pos, block.Air{}, 0)
	if (<-other.packets).(*packet.UpdateBlock).NewBlockRuntimeID != world.BlockRuntimeID(block.Air{}) {
		t.Error("private preview leaked to other session")
	}
	s.showBlockPreviews(0, 0, 0, 15)
	if (<-s.packets).(*packet.UpdateBlock).NewBlockRuntimeID != world.BlockRuntimeID(block.Glass{}) {
		t.Error("chunk replay lost glass")
	}
	s.showBlockPreviews(1, 0, 0, 15)
	s.showBlockPreviews(0, 0, 16, 31)
	if len(s.packets) != 0 {
		t.Error("replayed in wrong chunk or subchunk")
	}
	s.SetBlockPreviews(nil)
	s.ViewBlockUpdate(pos, block.Air{}, 0)
	if (<-s.packets).(*packet.UpdateBlock).NewBlockRuntimeID != world.BlockRuntimeID(block.Air{}) {
		t.Error("clearing preview did not restore real updates")
	}
}
