package player

import (
	"math"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// BorderDamageSource is the damage a player takes outside the world border.
type BorderDamageSource struct{}

func (BorderDamageSource) ReducedByArmour() bool     { return false }
func (BorderDamageSource) ReducedByResistance() bool { return true }
func (BorderDamageSource) Fire() bool                { return false }
func (BorderDamageSource) IgnoreTotem() bool         { return false }

// borderJump is how far a player has to move in one tick for it to count as a teleport, which
// lets go of a player outside a Region border.
const borderJump = 10

// borderHold is a Region border's hold on a player (world.BorderConfig's Region).
type borderHold struct {
	held  bool
	last  mgl64.Vec3
	world *world.World
}

// tickBorder shows the world border to the player's session and hurts a player outside it, as
// Java does: past the safe zone, DamagePerBlock for each block further out, at least 1.
func (p *Player) tickBorder(tx *world.Tx) {
	if !world.BordersSet() {
		return
	}
	b, pos, h := tx.World().Border(), p.Position(), &p.border
	if b.Region {
		switch {
		case b.Contains(pos):
			h.held = true
		case h.world != tx.World() || pos.Sub(h.last).Len() > borderJump:
			h.held = false
		}
		if !h.held {
			b = world.Border{} // not held: no border for this player
		}
	}
	h.world, h.last = tx.World(), pos
	if v, ok := p.conn().(world.BorderViewer); ok {
		v.ViewBorder(b, p.Position())
	}
	if !b.Enabled() || b.DamagePerBlock <= 0 || !p.GameMode().AllowsTakingDamage() {
		return
	}
	if d := b.DistanceOutside(p.Position()) - b.SafeZone; d > 0 {
		p.Hurt(max(1, math.Floor(d*b.DamagePerBlock)), BorderDamageSource{})
	}
}

// borderMove keeps a player inside the world border from walking out of it, as Java's border
// collision does: a move from inside to outside ends at the edge. It reports where the move
// ends instead, if it was changed.
func (p *Player) borderMove(from, to mgl64.Vec3) (mgl64.Vec3, bool) {
	if !world.BordersSet() {
		return to, false
	}
	b := p.tx.World().Border()
	if !b.Enabled() || !b.Contains(from) || b.Contains(to) {
		return to, false
	}
	return b.Clamp(to, 0.3), true
}
