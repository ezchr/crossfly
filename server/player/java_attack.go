package player

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// JavaAttack is what a Java Edition client knows of its player's next attack: Java's attack sounds depend
// on it, the attack itself (its damage, its knockback) does not.
type JavaAttack struct {
	// Strength is the attack strength from 0 to 1: how far the attack cooldown had recharged.
	Strength float64
	// Movement is how far the player moved horizontally in its last tick, in blocks.
	Movement float64
}

// SetNextJavaAttack sets the JavaAttack of the player's next AttackEntity call. Java viewers then hear the
// attack as vanilla Java plays it: weak below 90% strength, the knockback sound as well for a sprinting
// hit at full strength, and the sweep sound for a full-strength sword hit on the ground at walking speed.
// Without it (a Bedrock player) an attack is at full strength with neither: a Bedrock player keeps
// sprinting after a sprinting hit, where a Java player stops, so every hit of theirs would sound like one.
func (p *Player) SetNextJavaAttack(a JavaAttack) {
	p.nextJavaAttack, p.nextJavaAttackSet = a, true
}

// attackSound is the sound of an attack by the player: damaged is whether it hurt the target and critical
// whether it was a critical hit (Player.attack in vanilla Java). Bedrock viewers only hear whether it did
// damage.
func (p *Player) attackSound(damaged, critical bool) sound.Attack {
	java := p.nextJavaAttackSet
	strength, walking := 1.0, false
	if java {
		strength = p.nextJavaAttack.Strength
		walking = p.nextJavaAttack.Movement < p.Speed()*2.5
	}
	full := strength > 0.9
	s := sound.Attack{Damage: damaged, Weak: !full, Knockback: java && full && p.Sprinting()}
	if damaged {
		held, _ := p.HeldItems()
		_, sword := held.Item().(item.Sword)
		s.Critical = critical
		s.Sweep = full && walking && sword && !critical && !s.Knockback && p.OnGround()
	}
	return s
}
