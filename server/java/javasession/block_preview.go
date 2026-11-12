package javasession

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// SetBlockPreviews publishes an immutable snapshot owned by player.SetBlockPreviews.
func (s *Session) SetBlockPreviews(blocks map[cube.Pos]world.Block) {
	if len(blocks) == 0 {
		s.blockPreviews.Store(nil)
	} else {
		s.blockPreviews.Store(&blocks)
	}
}

// showBlockPreviews restores previews after a chunk or subchunk was sent to this client.
func (s *Session) showBlockPreviews(x, z int32, minY, maxY int) {
	if blocks := s.blockPreviews.Load(); blocks != nil {
		for pos, b := range *blocks {
			if int32(pos[0]>>4) == x && int32(pos[2]>>4) == z && pos[1] >= minY && pos[1] <= maxY {
				s.ViewBlockUpdate(pos, b, 0)
			}
		}
	}
}
