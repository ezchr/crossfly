package particle

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Vibration is the vibration signal a vibration listener (a warden, a sculk sensor or shrieker) receives:
// it flies from the position it is added at, the centre of the block the vibration came from, to the
// listener (BDS VibrationListener::_requestVibrationParticle: LevelEventGeneric 2027 with an origin, a
// target, speed 20 and a time to live; Java's VibrationParticleOption).
type Vibration struct {
	particle

	// Entity, if not nil, is the listener it flies to: its head (BDS attach position 3), EyeHeight
	// above its feet for Java. Without one it flies to Pos.
	Entity    *world.EntityHandle
	EyeHeight float64
	// Pos is the listener's block centre, for a block listener.
	Pos mgl64.Vec3
	// Ticks is the time it takes to arrive: one tick per block of distance.
	Ticks int
}

// Shriek is the shriek of a sculk shrieker, added at the shrieker's block (BDS: LevelEventGeneric 2035
// with originX, originY and originZ; Java: level event 3007).
type Shriek struct{ particle }
