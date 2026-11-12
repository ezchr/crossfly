package javasession

import (
	"bytes"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/world"
	"testing"
)

func TestBlockPreviewPrivateUpdatesAndChunkReplay(t *testing.T) {
	s, other := fxTestSession(), fxTestSession()
	s.blk = blocks()
	other.blk = blocks()
	pos := cube.Pos{1, 5, 1}
	s.SetBlockPreviews(map[cube.Pos]world.Block{pos: block.Glass{}})
	s.ViewBlockUpdate(pos, block.Air{}, 0)
	preview := s.take()
	other.ViewBlockUpdate(pos, block.Air{}, 0)
	real := other.take()
	if len(preview) != 1 || len(real) != 1 || preview[0].id != v777.ClientboundPlayBlockUpdate {
		t.Fatal("missing block updates")
	}
	if bytes.Equal(preview[0].w.B, real[0].w.B) {
		t.Error("private glass preview was not encoded")
	}
	s.showBlockPreviews(0, 0, 0, 15)
	replay := s.take()
	if len(replay) != 1 || !bytes.Equal(replay[0].w.B, preview[0].w.B) {
		t.Error("chunk replay lost preview")
	}
	s.showBlockPreviews(1, 0, 0, 15)
	if len(s.take()) != 0 {
		t.Error("replayed in wrong chunk")
	}
	s.SetBlockPreviews(nil)
	s.ViewBlockUpdate(pos, block.Air{}, 0)
	cleared := s.take()
	if len(cleared) != 1 || !bytes.Equal(cleared[0].w.B, real[0].w.B) {
		t.Error("clearing did not restore real block encoding")
	}
}
