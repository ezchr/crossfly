//go:build !crossfly_full

package player

import (
	"sync"

	"github.com/df-mc/dragonfly/server/world"
)

// Crossfly Core has no mobs: entity.Mobs is always false, so these are never used (player/mob_hooks.go in Crossfly
// Full).

var lastAttackers sync.Map

func (p *Player) recordAttacker(world.DamageSource) {}
