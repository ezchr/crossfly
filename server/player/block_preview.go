package player

import (
	"maps"

	"github.com/df-mc/dragonfly/server/block"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// SetBlockPreviews replaces this player's private, client-only block previews. It must be called in
// the player's transaction. Previews never change collision, world storage or other viewers. Clicks
// are delivered to the normal block handlers, but cannot break or place real blocks at a preview.
// Passing nil restores the real blocks. The caller may reuse its map after this method returns.
func (p *Player) SetBlockPreviews(blocks map[cube.Pos]world.Block) {
	old, oldWorld := p.blockPreviews, p.previewWorld
	p.blockPreviews = maps.Clone(blocks)
	p.previewWorld = p.tx.World()
	if p.conn() == session.Nop {
		return
	}
	if s, ok := p.conn().(interface {
		SetBlockPreviews(map[cube.Pos]world.Block)
	}); ok {
		s.SetBlockPreviews(p.blockPreviews)
	}
	sameWorld := oldWorld == p.tx.World()
	changed := map[cube.Pos]struct{}{}
	if sameWorld {
		for pos, b := range old {
			if nb, ok := blocks[pos]; !ok {
				p.resendNearbyBlock(pos)
				changed[pos] = struct{}{}
			} else if nb == nil || b == nil || world.BlockRuntimeID(nb) != world.BlockRuntimeID(b) {
				changed[pos] = struct{}{}
			}
		}
	}
	for pos, b := range blocks {
		if b != nil && p.withinChunkRadius(pos.Vec3()) {
			p.conn().ViewBlockUpdate(pos, b, 0)
			if _, ok := old[pos]; !ok || !sameWorld {
				changed[pos] = struct{}{}
			}
		}
	}
	p.resendPreviewNeighbours(changed)
}

// resendPreviewNeighbours sends the real blocks next to previews that appeared or went away, so
// that clients recompute the shapes that depend on their neighbours, such as glass pane, fence,
// wall and iron bar connections, and none stay joined to a preview that is gone.
func (p *Player) resendPreviewNeighbours(changed map[cube.Pos]struct{}) {
	done := map[cube.Pos]struct{}{}
	for pos := range changed {
		for _, f := range cube.Faces() {
			n := pos.Side(f)
			if _, ok := changed[n]; ok {
				continue
			}
			if _, ok := done[n]; ok {
				continue
			}
			done[n] = struct{}{}
			if p.hasBlockPreview(n) || !p.withinChunkRadius(n.Vec3()) {
				continue
			}
			if b, loaded := p.tx.BlockLoaded(n); !loaded {
				continue
			} else if _, air := b.(block.Air); air {
				continue
			}
			p.resendNearbyBlock(n)
		}
	}
}

func (p *Player) hasBlockPreview(pos cube.Pos) bool {
	return p.previewWorld == p.tx.World() && p.blockPreviews[pos] != nil
}

// previewClick only routes interactions; it deliberately never changes a world block or inventory.
func (p *Player) previewClick(pos cube.Pos, face cube.Face, click *mgl64.Vec3, finish bool) bool {
	if !p.hasBlockPreview(pos) {
		return false
	}
	p.AbortBreaking()
	if p.canReach(pos.Vec3Centre()) {
		ctx := NewEventContext(p.tx, p)
		if click != nil {
			p.Handler().HandleItemUseOnBlock(ctx, pos, face, *click)
		} else if finish {
			drops, xp := []item.Stack(nil), 0
			p.Handler().HandleBlockBreak(ctx, pos, &drops, &xp)
		} else {
			p.Handler().HandleStartBreak(ctx, pos)
		}
	}
	p.resendNearbyBlocks(pos, face)
	return true
}
