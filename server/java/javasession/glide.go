package javasession

import (
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
)

// Elytra gliding of the player's own Java client. A Java client starts gliding itself and then asks
// the server (START_FALL_FLYING), but it never says it stopped: a vanilla server ends the glide
// (LivingEntity.updateFallFlying: once canGlide no longer holds, shared flag 7 goes off) and the
// client follows the flag. A Bedrock client says StopGliding itself, so this is the Java side only.
// The parts of canGlide that are the server's own (the elytra worn, riding) the player's tick checks
// for both editions; the client's on-ground flag and levitation are checked here.

// moveFlagOnGround is the on_ground bit of the flags of the move packets.
const moveFlagOnGround = 0x01

// moveStatus handles move_player_status_only: only the client's flags changed.
func (s *Session) moveStatus(flags byte) {
	err := s.withPlayer(func(tx *world.Tx, c session.Controllable) {
		if s.pendingTeleport.Load() != 0 {
			return // as move: the client has not caught up with our teleport yet
		}
		s.landed(c, flags&moveFlagOnGround != 0)
	})
	if err != nil && !stopped(err) {
		s.log.Debug("move status", "err", err)
	}
}

// landed notes the on-ground flag of a move packet and ends the glide of a player that may no
// longer glide by it: on the ground, or levitating (canGlide).
func (s *Session) landed(c session.Controllable, onGround bool) {
	s.clientOnGround.Store(onGround)
	if c.Gliding() && (onGround || levitating(c)) {
		c.StopGliding() // sends the flags without the glide bit (ViewEntityState)
	}
}

// startGliding handles START_FALL_FLYING as Player.tryToStartFallFlying does: off the ground, not
// levitating, with an elytra (Player.StartGliding). A refused start sends the flags back, as
// vanilla's stopFallFlying does: the client glides already, by its own prediction, until told not to.
func (s *Session) startGliding(c session.Controllable) {
	if !s.clientOnGround.Load() && !levitating(c) {
		c.StartGliding()
	}
	if !c.Gliding() {
		s.viewSelfFlags(c)
	}
}

// levitating reports whether c has the levitation effect.
func levitating(c session.Controllable) bool {
	for _, e := range c.Effects() {
		if e.Type() == effect.Levitation {
			return true
		}
	}
	return false
}
