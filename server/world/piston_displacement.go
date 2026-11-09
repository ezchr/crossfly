//go:build crossfly_full

package world

import "github.com/go-gl/mathgl/mgl64"

// LimitPistonDisplacement bounds an entity's total piston displacement to .51 blocks per axis and game tick.
// Several blocks of a moving slime structure may otherwise move the same entity multiple times.
func (tx *Tx) LimitPistonDisplacement(e Entity, delta mgl64.Vec3) mgl64.Vec3 {
	w := tx.World()
	if w.pistonDisplacement == nil {
		w.pistonDisplacement = make(map[*EntityHandle]mgl64.Vec3)
	}
	previous := w.pistonDisplacement[e.H()]
	total := previous.Add(delta)
	for i := range total {
		total[i] = min(.51, max(-.51, total[i]))
	}
	w.pistonDisplacement[e.H()] = total
	return total.Sub(previous)
}
