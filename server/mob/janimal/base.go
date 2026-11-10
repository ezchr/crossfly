//go:build crossfly_full

package janimal

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/world"
)

// Base is embedded by the Behaviour of every Java animal: the AgeableMob and Animal parts of the class. The
// outer type sets Food (Animal.isFood) and may override any hook, calling Base's.
type Base struct {
	A Animal
	// Food is Animal.isFood.
	Food func(item.Stack) bool
	// LD is the last damage (LivingEntity.getLastDamageSource) PanicGoal reads.
	LD LastDamage
}

// LastDamageState ...
func (b *Base) LastDamageState() *LastDamage { return &b.LD }

// AnimalState ...
func (b *Base) AnimalState() *Animal { return &b.A }

// IsFood is Animal.isFood.
func (b *Base) IsFood(s item.Stack) bool { return !s.Empty() && b.Food != nil && b.Food(s) }

// CustomServerAiStep is Animal.customServerAiStep: an animal that is not a breedable adult is not in love.
func (b *Base) CustomServerAiStep(*javamob.Mob) { b.A.BeforeAI() }

// PostTick runs AgeableMob.aiStep and Animal.aiStep after the mob's AI: growth, the breeding cooldown, love
// time and the growth particles, and the baby flag when the boundary is crossed.
func (b *Base) PostTick(m *javamob.Mob) { TickAgeable(m, &b.A) }

// TickAgeable runs the Animal tick for m and shows its results.
func TickAgeable(m *javamob.Mob, a *Animal) {
	t := a.Tick(!m.Dead())
	if t.Boundary {
		m.SetBaby(a.Baby())
	}
	if t.HappyParticle {
		happyParticle(m)
	}
	if t.AgeLockParticle {
		ageLockParticle(m, t.AgeLocked)
	}
}

// OnHurt is Animal.actuallyHurt: damage ends love mode.
func (b *Base) OnHurt(m *javamob.Mob, _ float64, src world.DamageSource) {
	b.A.ResetLove()
	b.LD.Record(src, m.TickCount())
}

// RemoveWhenFarAway is Animal.removeWhenFarAway: animals never despawn.
func (b *Base) RemoveWhenFarAway(*javamob.Mob, float64) bool { return false }

// XPReward is Animal.getBaseExperienceReward: 1 to 3 (babies give none: the base checks).
func (b *Base) XPReward(m *javamob.Mob, _ int) int { return KillXP(m.Rand()) }

// AgeableGroupData is AgeableMob.AgeableMobGroupData: the pack members spawned so far and whether (and how
// likely) later members are babies.
type AgeableGroupData struct {
	Size            int
	ShouldSpawnBaby bool
	BabyChance      float32
}

// FinalizeSpawnGroup is AgeableMob.finalizeSpawn: the first mob of a pack (and every mob spawned on its
// own: a spawn egg, a command) is an adult, later pack members are babies 5% of the time.
func (b *Base) FinalizeSpawnGroup(m *javamob.Mob, _ javamob.SpawnReason, group any) any {
	return FinalizeAgeableGroup(m, &b.A, group)
}

// FinalizeAgeableGroup is the AgeableMob.finalizeSpawn part for any ageable behaviour.
func FinalizeAgeableGroup(m *javamob.Mob, a *Animal, group any) any {
	g, ok := group.(*AgeableGroupData)
	if !ok {
		g = &AgeableGroupData{ShouldSpawnBaby: true, BabyChance: 0.05}
	}
	if g.ShouldSpawnBaby && g.Size > 0 && m.Rand().Float32() <= g.BabyChance {
		a.SetBaby(true)
		m.SetBaby(true)
	}
	g.Size++
	return g
}

// MobInteract is Animal.mobInteract followed by AgeableMob.mobInteract (the golden dandelion).
func (b *Base) MobInteract(m *javamob.Mob, user item.User, held item.Stack) (item.Stack, bool) {
	return InteractAnimal(m, &b.A, user, held, b.IsFood(held), b.A.CanFallInLove())
}

// InteractAnimal is the feeding part of Animal.mobInteract with the class's isFood and canFallInLove answers,
// then the golden dandelion of AgeableMob.mobInteract.
func InteractAnimal(m *javamob.Mob, a *Animal, user item.User, held item.Stack, food, canFallInLove bool) (item.Stack, bool) {
	r, boundary := a.FeedIf(food, entityUUID(user), canFallInLove)
	if r.Used() {
		held = usePlayerItem(user, held)
		if boundary {
			m.SetBaby(a.Baby())
		}
		if r == FeedLove {
			for _, v := range m.Tx().Viewers(m.Position()) {
				v.ViewEntityAction(m.Mob, Event(EventLoveHearts))
			}
		}
		playEatingSound(m)
		return held, true
	}
	if isGoldenDandelion(held) && a.CanUseGoldenDandelion() {
		locked := a.ToggleAgeLock()
		m.SetBaby(true)
		held = usePlayerItem(user, held)
		if locked {
			m.SetPersistenceRequired()
		}
		playDandelionSound(m, locked)
		return held, true
	}
	return held, false
}

// EncodeNBT ...
func (b *Base) EncodeNBT(data map[string]any) { b.A.EncodeNBT(data) }

// DecodeNBT ...
func (b *Base) DecodeNBT(m *javamob.Mob, data map[string]any) {
	b.A.DecodeNBT(data)
	if m != nil && m.Baby() != b.A.Baby() {
		m.SetBaby(b.A.Baby())
	}
}
