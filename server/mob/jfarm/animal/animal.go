//go:build crossfly_full

package animal

import (
	"math/rand/v2"

	"github.com/google/uuid"
)

// Animal is the server state of a Java Animal (net.minecraft.world.entity.animal.Animal): an Ageable that
// can fall in love when fed and breed with a partner of its kind.
//
// A mob keeps one Animal in its per-mob state, calls Tick once per tick after its AI and asks Feed when a
// player interacts with it. The goals (BreedGoal, TemptGoal, FollowParentGoal, PanicGoal, EatBlockGoal)
// read it through the Host interface.
type Animal struct {
	Ageable
	// inLove: Animal.inLove, the ticks left in love mode (600 after feeding).
	inLove int32
	// loveCause: Animal.loveCause, the player who fed the animal (the breeder credited for the baby).
	loveCause uuid.UUID
}

const (
	// ParentAgeAfterBreeding is Animal.PARENT_AGE_AFTER_BREEDING: the 5 minute breeding cooldown.
	ParentAgeAfterBreeding int32 = 6000
	// InLoveTicks is the love mode time Animal.setInLove gives (30 seconds).
	InLoveTicks int32 = 600
	// EventLoveHearts is EntityEvent.IN_LOVE_HEARTS (18): the client shows 7 hearts.
	EventLoveHearts byte = 18
)

// InLove is Animal.isInLove.
func (a *Animal) InLove() bool { return a.inLove > 0 }

// InLoveTime is Animal.getInLoveTime.
func (a *Animal) InLoveTime() int32 { return a.inLove }

// SetInLoveTime is Animal.setInLoveTime.
func (a *Animal) SetInLoveTime(t int32) { a.inLove = t }

// LoveCause is the UUID of the player that made the animal fall in love (uuid.Nil if none).
func (a *Animal) LoveCause() uuid.UUID { return a.loveCause }

// CanFallInLove is Animal.canFallInLove: not in love already.
func (a *Animal) CanFallInLove() bool { return a.inLove <= 0 }

// SetInLove is Animal.setInLove: 600 ticks of love mode, remembering the player (uuid.Nil for none, as
// a dispenser or an allay). The caller broadcasts EventLoveHearts.
func (a *Animal) SetInLove(cause uuid.UUID) {
	a.inLove = InLoveTicks
	if cause != uuid.Nil {
		a.loveCause = cause
	}
}

// ResetLove is Animal.resetLove (also run when the animal is hurt: Animal.actuallyHurt).
func (a *Animal) ResetLove() { a.inLove = 0 }

// BeforeAI is the Animal.customServerAiStep part that runs before the goals: an animal that is not
// a breedable adult (a baby or on cooldown) drops out of love.
func (a *Animal) BeforeAI() {
	if a.age != 0 {
		a.inLove = 0
	}
}

// Tick runs the server half of AgeableMob.aiStep and Animal.aiStep once per tick, after the AI: growth
// and the cooldown (Ageable.Tick), then the love timer. The hearts Animal.aiStep adds every 10 ticks are
// client-side particles (Level.addParticle does nothing on a ServerLevel), so nothing is sent for them.
func (a *Animal) Tick(alive bool) AgeTick {
	t := a.Ageable.Tick(alive)
	if a.age != 0 {
		a.inLove = 0
	}
	if a.inLove > 0 {
		a.inLove--
	}
	return t
}

// CanMate is Animal.canMate for two animals of the same class (the caller checks the class, and that
// partner is not the animal itself): both are in love.
func (a *Animal) CanMate(partner *Animal) bool {
	return partner != a && a.InLove() && partner.InLove()
}

// FinishBreeding is the state part of Animal.finalizeSpawnChildFromBreeding, run on both parents: the
// 5 minute cooldown and no more love. The caller broadcasts EventLoveHearts on the animal that ran the
// BreedGoal and drops BreedingXP experience if the mobDrops game rule is on.
func (a *Animal) FinishBreeding() {
	a.SetAge(ParentAgeAfterBreeding)
	a.ResetLove()
}

// BreedingXP is the experience orb Animal.finalizeSpawnChildFromBreeding drops: 1 to 7.
func BreedingXP(r *rand.Rand) int { return r.IntN(7) + 1 }

// KillXP is Animal.getBaseExperienceReward: 1 to 3 (an adult; Mob.getExperienceReward gives babies none
// through the base class).
func KillXP(r *rand.Rand) int { return 1 + r.IntN(3) }

// FeedResult is what Animal.mobInteract did with the food in a player's hand. Food the animal cannot use
// now (an adult in love or on cooldown) is FeedPass: the server falls through to Mob.mobInteract
// (InteractionResult.CONSUME is only the client's answer).
type FeedResult uint8

const (
	// FeedPass: the item is not food, or the animal cannot use it: interaction continues
	// (AgeableMob golden dandelion, then Mob.mobInteract, then the item's interactLivingEntity).
	FeedPass FeedResult = iota
	// FeedLove: one item used (Mob.usePlayerItem), the animal is in love, eating sound played.
	FeedLove
	// FeedGrow: one item used, the baby grew by 10% of its remaining time, eating sound played.
	FeedGrow
)

// Used reports whether the result uses one item from the player's hand.
func (r FeedResult) Used() bool { return r == FeedLove || r == FeedGrow }

// Feed is Animal.mobInteract for an item that is (food) or is not the animal's food, held by player.
// Love mode needs an adult off cooldown that is not in love; otherwise a baby grows. The caller uses up
// the item (Mob.usePlayerItem: not in creative), plays the eating sound, broadcasts EventLoveHearts for
// FeedLove and refreshes the entity data on a boundary.
func (a *Animal) Feed(food bool, player uuid.UUID) (FeedResult, bool) {
	if !food {
		return FeedPass, false
	}
	if a.age == 0 && a.CanFallInLove() {
		a.SetInLove(player)
		return FeedLove, false
	}
	if a.CanAgeUp() {
		boundary := a.AgeUp(SpeedUpSecondsWhenFeeding(-a.age), true)
		return FeedGrow, boundary
	}
	return FeedPass, false
}

// EncodeNBT adds the AgeableMob and Animal save data to m.
func (a *Animal) EncodeNBT(m map[string]any) {
	a.Ageable.EncodeNBT(m)
	m["InLove"] = a.inLove
	if a.loveCause != uuid.Nil {
		m["LoveCause"] = uuidInts(a.loveCause)
	}
}

// DecodeNBT reads what EncodeNBT wrote.
func (a *Animal) DecodeNBT(m map[string]any) {
	a.Ageable.DecodeNBT(m)
	a.inLove = nbtInt(m, "InLove", 0)
	a.loveCause = nbtUUID(m, "LoveCause")
}
