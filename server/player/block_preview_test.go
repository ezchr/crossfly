package player

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"testing"
)

type previewHandler struct {
	NopHandler
	start, finish, use int
}

func (h *previewHandler) HandleStartBreak(*Context, cube.Pos)                            { h.start++ }
func (h *previewHandler) HandleBlockBreak(*Context, cube.Pos, *[]item.Stack, *int)       { h.finish++ }
func (h *previewHandler) HandleItemUseOnBlock(*Context, cube.Pos, cube.Face, mgl64.Vec3) { h.use++ }

func TestBlockPreviewClicksDoNotChangeWorldOrInventory(t *testing.T) {
	spearWorld(t, world.GameModeCreative, func(tx *world.Tx, p *Player) {
		h := &previewHandler{}
		p.Handle(h)
		air, solid, far := cube.Pos{1, 1, 0}, cube.Pos{1, 1, 1}, cube.Pos{1000, 1, 0}
		tx.SetBlock(solid, block.Stone{}, nil)
		previews := map[cube.Pos]world.Block{air: block.Glass{}, solid: block.Glass{}, far: block.Glass{}}
		p.SetBlockPreviews(previews)
		delete(previews, air)
		p.SetHeldItems(item.NewStack(block.Glass{}, 10), item.Stack{})
		for _, pos := range []cube.Pos{air, solid} {
			p.StartBreaking(pos, cube.FaceUp)
			p.BreakBlock(pos)
			p.UseItemOnBlock(pos, cube.FaceUp, mgl64.Vec3{.5, 1, .5})
		}
		p.StartBreaking(far, cube.FaceUp)
		p.BreakBlock(far)
		p.UseItemOnBlock(far, cube.FaceUp, mgl64.Vec3{})
		if h.start != 2 || h.finish != 2 || h.use != 2 {
			t.Errorf("click counts: %+v", h)
		}
		if _, ok := tx.Block(air).(block.Air); !ok {
			t.Error("preview became a real block")
		}
		if _, ok := tx.Block(solid).(block.Stone); !ok {
			t.Error("preview removed a real block")
		}
		main, _ := p.HeldItems()
		if main.Count() != 10 {
			t.Error("preview changed held stack")
		}
		p.SetBlockPreviews(nil)
		if p.hasBlockPreview(air) {
			t.Error("preview survived clearing")
		}
		p.BreakBlock(solid)
		if _, ok := tx.Block(solid).(block.Air); !ok {
			t.Error("ordinary breaking no longer works")
		}
	})
}
