//go:build crossfly_full

package entity

import "github.com/go-gl/mathgl/mgl64"

// MoveByPiston applies an already collision-clipped physical displacement without replacing the entity's velocity.
func (e *Ent) MoveByPiston(delta mgl64.Vec3) { e.Teleport(e.Position().Add(delta)) }
