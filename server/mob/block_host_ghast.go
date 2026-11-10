//go:build crossfly_full

package mob

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// GhastlingEmerged gives a ghastling that came out of a dried ghast its home at the block, as the 26.60
// DriedGhastBlock sets the home of the baby it spawns, and plays its spawn sound.
func (BlockHost) GhastlingEmerged(tx *world.Tx, ghastling world.Entity, home cube.Pos) {
	m, ok := Of(ghastling)
	if !ok {
		return
	}
	m.SetHome(home)
	m.PlaySound("spawn")
}
