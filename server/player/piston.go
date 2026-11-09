//go:build crossfly_full

package player

import "github.com/go-gl/mathgl/mgl64"

// MoveByPiston applies an already collision-clipped piston displacement. This is a forced physical move, so it
// preserves velocity, fall state and riding state instead of invoking a gameplay teleport or dismounting.
func (p *Player) MoveByPiston(delta mgl64.Vec3) {
	pos := p.data.Pos.Add(delta)
	for _, viewer := range p.viewers() {
		viewer.ViewEntityTeleport(p, pos)
	}
	p.data.Pos = pos
}
