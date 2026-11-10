//go:build crossfly_full

package janimal

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// MobGriefing and MobDrops are the game rules (Crossfly has no game rule store yet: both on, as on a fresh
// Paper world). Servers may replace them.
var (
	MobGriefing = func(*world.Tx) bool { return true }
	MobDrops    = func(*world.Tx) bool { return true }
)

func gameRuleMobGriefing(tx *world.Tx) bool { return MobGriefing(tx) }
func gameRuleMobDrops(tx *world.Tx) bool    { return MobDrops(tx) }

// Attributes is Animal.createAnimalAttributes: the mob attributes plus a tempt range of 10.
func Attributes() map[javamob.Attribute]float64 {
	a := javamob.LivingAttributes()
	a[javamob.AttrTemptRange] = 10
	return a
}

// JavaDamageType is the Java damage type id of a Dragonfly damage source ("" if unknown), for damage type
// tags such as PanicCauses.
func JavaDamageType(src world.DamageSource) string {
	switch s := src.(type) {
	case entity.AttackDamageSource:
		if _, ok := s.Attacker.(interface{ GameMode() world.GameMode }); ok {
			return "player_attack"
		}
		return "mob_attack"
	case entity.ProjectileDamageSource:
		if s.Projectile != nil {
			switch s.Projectile.H().Type().EncodeEntity() {
			case "minecraft:arrow", "minecraft:spectral_arrow":
				return "arrow"
			case "minecraft:trident":
				return "trident"
			case "minecraft:wind_charge", "minecraft:breeze_wind_charge":
				return "wind_charge"
			case "minecraft:fireball", "minecraft:small_fireball":
				return "fireball"
			case "minecraft:fireworks_rocket":
				return "fireworks"
			}
		}
		return "thrown"
	case entity.ExplosionDamageSource:
		return "explosion"
	case entity.LightningDamageSource:
		return "lightning_bolt"
	case entity.FallDamageSource:
		return "fall"
	case entity.DrowningDamageSource:
		return "drown"
	case entity.SuffocationDamageSource:
		return "in_wall"
	case entity.VoidDamageSource:
		return "out_of_world"
	case block.FireDamageSource:
		return "in_fire"
	case block.LavaDamageSource:
		return "lava"
	case block.MagmaDamageSource:
		return "hot_floor"
	case block.DamageSource:
		return "cactus"
	case effect.WitherDamageSource:
		return "wither"
	case effect.PoisonDamageSource, effect.InstantDamageSource:
		return "magic"
	}
	if f, ok := src.(interface{ Fire() bool }); ok && f.Fire() {
		return "on_fire"
	}
	return ""
}

// LastDamage keeps LivingEntity.lastDamageSource and lastDamageStamp for getLastDamageSource (40 ticks).
type LastDamage struct {
	typ   string
	stamp int
	set   bool
}

// Record notes damage of src at tick (HurtListener.OnHurt).
func (l *LastDamage) Record(src world.DamageSource, tick int) {
	l.typ, l.stamp, l.set = JavaDamageType(src), tick, true
}

// Type is the Java damage type of the last damage within 40 ticks of now, "" if none.
func (l *LastDamage) Type(now int) string {
	if !l.set || now-l.stamp > 40 {
		return ""
	}
	return l.typ
}

func lastDamageType(m *javamob.Mob) string {
	if b, ok := m.Behaviour().(interface{ LastDamageState() *LastDamage }); ok {
		return b.LastDamageState().Type(m.TickCount())
	}
	return ""
}

// happyParticle is the forced growth particle (ParticleTypes.HAPPY_VILLAGER at a random spot of the mob).
func happyParticle(m *javamob.Mob) {
	m.Tx().AddParticle(randomIn(m, 0.5), particle.Named{Bedrock: "minecraft:villager_happy", Java: "happy_villager"})
}

// ageLockParticle is AgeableMob.makeAgeLockedParticle: pause_mob_growth or reset_mob_growth above the mob.
// Bedrock has no such particles and is shown the happy villager one.
func ageLockParticle(m *javamob.Mob, locked bool) {
	j, y := "reset_mob_growth", m.Height()
	if locked {
		j, y = "pause_mob_growth", m.Height()+0.2
	}
	m.Tx().AddParticle(randomIn(m, y), particle.Named{Bedrock: "minecraft:villager_happy", Java: j})
}

func randomIn(m *javamob.Mob, yOff float64) mgl64.Vec3 {
	r := m.Rand()
	w := m.Width()
	p := m.Position()
	return mgl64.Vec3{p[0] + (r.Float64()*2-1)*w, p[1] + r.Float64()*m.Height() + yOff, p[2] + (r.Float64()*2-1)*w}
}

// eatingSounder is Animal.playEatingSound (a sound only the classes that override it have).
type eatingSounder interface{ PlayEatingSound(m *javamob.Mob) }

func playEatingSound(m *javamob.Mob) {
	if e, ok := m.Behaviour().(eatingSounder); ok {
		e.PlayEatingSound(m)
	}
}

func isGoldenDandelion(s item.Stack) bool {
	return !s.Empty() && JavaItemName(s.Item()) == "golden_dandelion"
}

func playDandelionSound(m *javamob.Mob, locked bool) {
	name := "item.golden_dandelion.unuse"
	if locked {
		name = "item.golden_dandelion.use"
	}
	m.Tx().PlaySound(m.Position(), sound.Custom{Name: name, Volume: 1, Pitch: 1})
}
