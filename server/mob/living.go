//go:build crossfly_full

package mob

import (
	"math"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

var _ entity.Living = (*Mob)(nil)

// hurtImmunity is the number of ticks after a hit during which only damage
// above that hit's counts (BDS/Java invulnerableTime 20, checked > 10).
const hurtImmunity = 10

func (m *Mob) Health() float64    { return m.s.health.Health() }
func (m *Mob) MaxHealth() float64 { return m.s.health.MaxHealth() }

// SetMaxHealth changes the maximum health (minecraft:health "max").
func (m *Mob) SetMaxHealth(v float64) {
	m.s.health.SetMaxHealth(v)
	m.viewHealth()
}

// healthViewer is a viewer that shows mob health (server/session sends
// UpdateAttributes). It is not part of world.Viewer.
type healthViewer interface{ ViewEntityHealth(e world.Entity) }

// viewHealth shows the mob's health to viewers that show it.
func (m *Mob) viewHealth() {
	tx := m.Tx()
	if tx == nil || tx.Closed() {
		return
	}
	for _, v := range tx.Viewers(m.data.Pos) {
		if hv, ok := v.(healthViewer); ok {
			hv.ViewEntityHealth(m)
		}
	}
}

// SetHealth sets the current health, within [0, MaxHealth]. It does not
// kill the mob; use Hurt for that.
func (m *Mob) SetHealth(v float64) {
	m.s.health.AddHealth(v - m.s.health.Health())
	m.viewHealth()
}

// Dead reports whether the mob died (it lies for deathTicks before it is
// removed).
func (m *Mob) Dead() bool { return m.s.dying > 0 || m.s.health.Health() <= 0 }

// Speed is the movement attribute.
func (m *Mob) Speed() float64     { return m.Attr(AttrMovement) }
func (m *Mob) SetSpeed(v float64) { m.s.attrs.SetBase(AttrMovement, v) }

func (m *Mob) AddEffect(e effect.Effect) {
	if m.immuneToEffect(e.Type()) {
		return
	}
	m.s.effects.Add(e, m)
}
func (m *Mob) RemoveEffect(t effect.Type)                 { m.s.effects.Remove(t, m) }
func (m *Mob) Effects() []effect.Effect                   { return m.s.effects.Effects() }
func (m *Mob) Effect(t effect.Type) (effect.Effect, bool) { return m.s.effects.Effect(t) }

// Heal heals the mob, up to its maximum health.
func (m *Mob) Heal(health float64, _ world.HealingSource) float64 {
	if m.Dead() || health <= 0 {
		return 0
	}
	before := m.Health()
	m.s.health.AddHealth(health)
	m.viewHealth()
	return m.Health() - before
}

// KnockBack pushes the mob away from src. Package mob/physics replaces it
// (SetKnockBack); the default is the player's knock back scaled by the
// knockback_resistance attribute.
func (m *Mob) KnockBack(src mgl64.Vec3, force, height float64) {
	if m.Dead() {
		return
	}
	if knockBackHook != nil {
		knockBackHook(m, src, force, height)
		return
	}
	v := m.Position().Sub(src)
	v[1] = 0
	if v.Len() != 0 {
		v = v.Normalize().Mul(force)
	}
	v[1] = height
	m.SetVelocity(v.Mul(1 - m.Attr(AttrKnockbackResistance)))
	m.SyncMovement()
}

// attackerOf returns the entity responsible for src.
func attackerOf(src world.DamageSource) world.Entity {
	switch src := src.(type) {
	case entity.AttackDamageSource:
		return src.Attacker
	case entity.ProjectileDamageSource:
		if src.Owner != nil {
			return src.Owner
		}
		return src.Projectile
	}
	return nil
}

// Hurt damages the mob:
//  1. DamageModifier components, then damage hooks, may change or cancel
//     the damage;
//  2. fire resistance ignores fire damage; armour (ArmourValue), the
//     protection enchantments and resistance lower the rest;
//  3. within hurtImmunity ticks of the last hit only damage above it counts;
//  4. health drops, the attacker becomes LastHurtBy, viewers see the hurt
//     animation, HurtListener components are told;
//  5. at 0 health the mob dies: DeathListener components run and it lies for
//     deathTicks before it is removed.
func (m *Mob) Hurt(dmg float64, src world.DamageSource) (float64, world.HurtResult) {
	m.ensureInit()
	s := m.s
	if m.Dead() || dmg < 0 {
		return 0, world.HurtIgnored
	}
	d := &Damage{Amount: dmg, Raw: dmg, Source: src, Attacker: attackerOf(src)}
	if m.riderInvulnerable(d.Attacker) {
		return 0, world.HurtIgnored
	}
	// HealthAttributeDelegate::getBuffValueWithModifiers: armour, then the
	// protection enchantments, come before the damage sensor's multiplier
	// and the actor's own damage rules (a wither's lethal hit, a witch's
	// magic resistance).
	if src.ReducedByArmour() {
		d.Amount = m.armourReduced(d.Amount)
	}
	d.Amount = m.protectionReduced(d.Amount, src)
	for _, c := range s.comps {
		if dm, ok := c.comp.(DamageModifier); ok {
			dm.ModifyDamage(m, d)
			if d.Cancel {
				return 0, world.HurtIgnored
			}
		}
	}
	if m.closed() {
		// A damage_sensor event removed the mob (it transformed).
		return 0, world.HurtIgnored
	}
	for _, h := range damageHooks {
		if h.Modify(m, d); d.Cancel {
			return 0, world.HurtIgnored
		}
	}
	if _, ok := s.effects.Effect(effect.FireResistance); ok && src.Fire() {
		return 0, world.HurtIgnored
	}
	total := d.Amount
	if res, ok := s.effects.Effect(effect.Resistance); ok && src.ReducedByResistance() {
		total *= math.Max(0, 1-0.2*float64(res.Level()))
	}
	total = math.Max(total, 0)
	dealt := total
	if s.immune > 0 {
		if dealt -= s.lastDamage; dealt <= 0 {
			return 0, world.HurtIgnored
		}
	} else {
		s.immune = hurtImmunity
	}
	s.lastDamage = total
	s.health.AddHealth(-dealt)
	s.noAction = 0
	m.viewHealth()
	if src.ReducedByArmour() {
		m.hurtArmour(d.Raw)
	}

	if d.Attacker != nil && d.Attacker.H() != m.H() {
		m.SetLastHurtBy(d.Attacker)
	}
	if !d.NoHurtAnimation {
		for _, v := range m.Tx().Viewers(m.Position()) {
			v.ViewEntityAction(m, entity.HurtAction{})
		}
	}
	for _, c := range s.comps {
		if l, ok := c.comp.(HurtListener); ok {
			l.OnHurt(m, dealt, src)
		}
	}
	for _, h := range hurtHooks {
		h.Hurt(m, dealt, src)
	}
	if s.health.Health() <= 0 && s.dying == 0 {
		m.die(src)
	} else if !d.NoHurtAnimation {
		m.PlaySound(SoundHurt)
	}
	return dealt, world.HurtAccepted
}

// Kill kills the mob outright with src.
func (m *Mob) Kill(src world.DamageSource) {
	if m.Dead() {
		return
	}
	m.s.health.AddHealth(-m.s.health.Health())
	m.viewHealth()
	m.die(src)
}

func (m *Mob) die(src world.DamageSource) {
	s := m.s
	s.dying = 1
	s.goals.StopAll(m)
	s.nav.Stop(m)
	s.move.Stop()
	for _, v := range m.Tx().Viewers(m.Position()) {
		v.ViewEntityAction(m, entity.DeathAction{})
	}
	m.PlaySound(SoundDeath)
	for _, c := range s.comps {
		if l, ok := c.comp.(DeathListener); ok {
			l.OnDeath(m, src)
		}
	}
	if m.closed() {
		// A death listener replaced the mob (a transformation at death,
		// villager to zombie villager): no drops or death hooks.
		return
	}
	for _, h := range deathHooks {
		h.Die(m, src)
	}
}

// Explode is called by explosions near the mob (entity.Ent.Explode): it
// hurts the mob with an entity.ExplosionDamageSource (Dragonfly's player
// formula, floor((impact^2+impact)*3.5*size*2+1); damage_sensor sees it) and
// pushes it away, scaled by knockback_resistance.
func (s *state) Explode(e *entity.Ent, src world.ExplosionSource, impact float64) {
	m := &Mob{Ent: e, data: s.data, s: s}
	if m.Dead() {
		return
	}
	m.Hurt(math.Floor((impact*impact+impact)*3.5*src.Size()*2+1), entity.ExplosionDamageSource{Source: src})
	if m.closed() {
		return
	}
	push := m.Position().Sub(src.Position())
	if push.Len() != 0 {
		push = push.Normalize()
	}
	m.SetVelocity(m.Velocity().Add(push.Mul(impact * (1 - m.Attr(AttrKnockbackResistance)))))
}
