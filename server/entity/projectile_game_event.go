package entity

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/gameevent"
	"github.com/go-gl/mathgl/mgl64"
)

// Projectiles tell the game event listeners who shot them: a sculk shrieker shrieks for the player
// behind a projectile, and projectiles standing on sculk sensors do not touch them.
func init() { gameevent.SetProjectileOwner(projectileOwner) }

// projectileOwner reports whether e is a projectile and returns its shooter, if it still exists.
func projectileOwner(tx *world.Tx, e world.Entity) (world.Entity, bool) {
	ent, ok := e.(*Ent)
	if !ok {
		return nil, false
	}
	lt, ok := ent.Behaviour().(*ProjectileBehaviour)
	if !ok {
		return nil, false
	}
	if h := lt.Owner(); h != nil {
		if o, ok := h.Entity(tx); ok {
			return o, true
		}
	}
	return nil, true
}

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
