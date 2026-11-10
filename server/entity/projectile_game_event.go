package entity

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/gameevent"
	"github.com/go-gl/mathgl/mgl64"
)

// postGameEvent posts projectile_shoot or projectile_land for the projectile e at pos, with its shooter as
// the owner (ProjectileComponent::shoot and ::onHit). Nothing happens in a world without vibration
// listeners.
func (lt *ProjectileBehaviour) postGameEvent(e *Ent, tx *world.Tx, ev gameevent.Event, pos mgl64.Vec3) {
	if !gameevent.Active(tx) {
		return
	}
	ctx := gameevent.Context{Source: e}
	if h := lt.Owner(); h != nil {
		if o, ok := h.Entity(tx); ok {
			ctx.Owner = o
		}
	}
	gameevent.PostContext(tx, ev, pos, ctx)
}
