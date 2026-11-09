//go:build crossfly_full

package player

import (
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

// Hooks for mobs: entity inventory windows (a horse's saddle, armour and chest) and who last
// hurt the player (pets defend their owner: behavior.owner_hurt_by_target).

// OpenEntityContainer opens the inventory window of e (entity.WindowOf) for the player, if its
// session can show one, and reports whether it did. Both the Bedrock and the Java session can.
func (p *Player) OpenEntityContainer(e world.Entity) bool {
	if o, ok := p.conn().(interface {
		OpenEntityContainer(e world.Entity, tx *world.Tx) bool
	}); ok {
		return o.OpenEntityContainer(e, p.tx)
	}
	return false
}

// lastAttackers holds the entity that last hurt each player (by handle).
var lastAttackers sync.Map

type attackRecord struct {
	attacker *world.EntityHandle
	at       time.Time
}

// recordAttacker remembers who dealt damage from src (an attack, or the owner of a projectile).
func (p *Player) recordAttacker(src world.DamageSource) {
	var attacker world.Entity
	switch s := src.(type) {
	case entity.AttackDamageSource:
		attacker = s.Attacker
	case entity.ProjectileDamageSource:
		attacker = s.Owner
	}
	if attacker == nil || attacker.H() == p.H() {
		return
	}
	lastAttackers.Store(p.H(), attackRecord{attacker: attacker.H(), at: time.Now()})
}

// LastAttacker returns the entity that last hurt the player (an attacker or the owner of a
// projectile) and when, as BDS keeps an actor's last hurt by mob.
func (p *Player) LastAttacker() (*world.EntityHandle, time.Time, bool) {
	v, ok := lastAttackers.Load(p.H())
	if !ok {
		return nil, time.Time{}, false
	}
	r := v.(attackRecord)
	return r.attacker, r.at, true
}
