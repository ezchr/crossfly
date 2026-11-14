package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
)

// previewRecorder records the blocks sent to a player; everything else is the no-op session.
type previewRecorder struct {
	*session.Session
	sent []sentPreviewBlock
}

type sentPreviewBlock struct {
	pos cube.Pos
	b   world.Block
}

func (r *previewRecorder) ViewBlockUpdate(pos cube.Pos, b world.Block, layer int) {
	if layer == 0 {
		r.sent = append(r.sent, sentPreviewBlock{pos, b})
	}
}
func (r *previewRecorder) SetBlockPreviews(map[cube.Pos]world.Block) {}
func (r *previewRecorder) ChunkRadius() int32                        { return 8 }

func (r *previewRecorder) last(pos cube.Pos) (world.Block, int) {
	var b world.Block
	n := 0
	for _, s := range r.sent {
		if s.pos == pos {
			b, n = s.b, n+1
		}
	}
	return b, n
}

func TestBlockPreviewsRefreshNeighbours(t *testing.T) {
	spearWorld(t, world.GameModeCreative, func(tx *world.Tx, p *Player) {
		rec := &previewRecorder{Session: session.Nop}
		p.s = rec
		defer func() { p.s = nil }()
		cell, pane, stone, far := cube.Pos{1, 0, 1}, cube.Pos{1, 0, 2}, cube.Pos{1, 1, 1}, cube.Pos{1, 0, 4}
		tx.SetBlock(pane, block.GlassPane{}, nil)
		tx.SetBlock(stone, block.Stone{}, nil)
		tx.SetBlock(far, block.GlassPane{}, nil)
		real := world.BlockRuntimeID(tx.Block(pane))

		p.SetBlockPreviews(map[cube.Pos]world.Block{cell: block.Glass{}})
		if b, _ := rec.last(cell); b == nil || world.BlockRuntimeID(b) != world.BlockRuntimeID(block.Glass{}) {
			t.Errorf("preview not sent: %v", b)
		}
		for _, n := range []cube.Pos{pane, stone} {
			if _, c := rec.last(n); c == 0 {
				t.Errorf("neighbour %v not refreshed when the preview appeared", n)
			}
		}
		if _, c := rec.last(far); c != 0 {
			t.Error("refreshed a block that does not touch the preview")
		}

		// An unchanged redraw sends no neighbours again.
		rec.sent = nil
		p.SetBlockPreviews(map[cube.Pos]world.Block{cell: block.Glass{}})
		if _, c := rec.last(pane); c != 0 {
			t.Error("unchanged preview refreshed its neighbours")
		}

		rec.sent = nil
		p.SetBlockPreviews(nil)
		if b, _ := rec.last(cell); b == nil || world.BlockRuntimeID(b) != world.BlockRuntimeID(block.Air{}) {
			t.Errorf("real block not restored: %v", b)
		}
		b, c := rec.last(pane)
		if c == 0 || world.BlockRuntimeID(b) != real {
			t.Errorf("pane not resent in its real state after the preview went: %v", b)
		}
		if _, c := rec.last(stone); c == 0 {
			t.Error("solid neighbour not refreshed after the preview went")
		}
	})
}
