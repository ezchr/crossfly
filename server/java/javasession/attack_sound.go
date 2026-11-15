package javasession

import (
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// attackSound plays an attack as vanilla Java's Player.attack does: the knockback sound first for a
// sprinting full-strength attack, then the no-damage sound for an attack that hurt nothing, or else the
// crit, sweep, weak or strong sound. A swing at the air makes no sound on Java (handlePunch only swings).
func (s *Session) attackSound(pos mgl64.Vec3, a sound.Attack) {
	attackSoundsOf(a, func(snd jsound) { s.sound(pos, snd, srcPlayers, 1, 1) })
}

// attackSoundsOf passes the sounds of an attack to play, in order.
func attackSoundsOf(a sound.Attack, play func(jsound)) {
	if a.Air {
		return
	}
	if a.Knockback {
		play(sndAttackKnockback)
	}
	snd := sndAttackStrong
	switch {
	case !a.Damage:
		snd = sndAttackNoDamage
	case a.Critical:
		snd = sndAttackCrit
	case a.Sweep:
		snd = sndAttackSweep
	case a.Weak:
		snd = sndAttackWeak
	}
	play(snd)
}

// playerHurtSound plays the hurt sound of another player that was hurt. A Java client plays the hurt
// sound of a damage_event only for its own player (ClientLevel.playSeededSound plays nothing for other
// entities): a vanilla server sends the hurt sound of a player to everyone else as a sound. Fire and
// drowning have hurt sounds of their own (sound.Burning, sound.Drowning), and a hit that kills plays the
// death sound instead (playerDeathSound).
func (s *Session) playerHurtSound(e world.Entity, src world.DamageSource) {
	p, ok := e.(*player.Player)
	if !ok || p.Dead() {
		return
	}
	if src != nil && src.Fire() {
		return
	}
	if _, drowning := src.(entity.DrowningDamageSource); drowning {
		return
	}
	s.sound(e.Position(), sndPlayerHurt, srcPlayers, 1, voicePitch())
}

// playerDeathSound plays the death sound of a player that died: the client plays none for the DEATH
// entity event of another entity, and the server sends none of its own player.
func (s *Session) playerDeathSound(e world.Entity) {
	if _, ok := e.(*player.Player); ok {
		s.sound(e.Position(), sndPlayerDeath, srcPlayers, 1, voicePitch())
	}
}

// voicePitch is the pitch of a grown-up living entity's sounds (LivingEntity.getVoicePitch).
func voicePitch() float32 { return (rnd()-rnd())*0.2 + 1 }
