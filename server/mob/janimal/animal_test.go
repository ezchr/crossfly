//go:build crossfly_full

package janimal

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/google/uuid"
)

// A baby grows up after exactly 24000 ticks (AgeableMob.aiStep), flagging the boundary once.
func TestBabyGrowsUpIn24000Ticks(t *testing.T) {
	var a Animal
	a.SetBaby(true)
	boundaries := 0
	for i := 0; i < 24000; i++ {
		if !a.Baby() {
			t.Fatalf("grew up after %d ticks, want 24000", i)
		}
		if a.Tick(true).Boundary {
			boundaries++
		}
	}
	if a.Baby() || boundaries != 1 {
		t.Fatalf("baby=%v boundaries=%d after 24000 ticks", a.Baby(), boundaries)
	}
}

// Breeding: love for 600 ticks, then a 6000 tick cooldown in which food does nothing.
func TestLoveAndBreedingCooldown(t *testing.T) {
	var a, b Animal
	p := uuid.New()
	if r, _ := a.Feed(true, p); r != FeedLove {
		t.Fatalf("adult feed = %v, want love", r)
	}
	if r, _ := a.Feed(true, p); r != FeedPass {
		t.Fatalf("feeding an animal in love = %v, want pass", r)
	}
	if a.CanMate(&b) {
		t.Fatal("mates with a partner not in love")
	}
	b.Feed(true, uuid.Nil)
	if !a.CanMate(&b) || a.CanMate(&a) {
		t.Fatal("mating rule")
	}
	if a.LoveCause() != p {
		t.Fatal("love cause not kept")
	}
	a.FinishBreeding()
	b.FinishBreeding()
	if a.InLove() || a.Age() != ParentAgeAfterBreeding {
		t.Fatalf("after breeding: inLove=%v age=%d", a.InLove(), a.Age())
	}
	for i := int32(0); i < ParentAgeAfterBreeding; i++ {
		if r, _ := a.Feed(true, p); r != FeedPass {
			t.Fatalf("fed on cooldown at tick %d: %v", i, r)
		}
		a.Tick(true)
	}
	if r, _ := a.Feed(true, p); r != FeedLove {
		t.Fatalf("after 6000 ticks feed = %v, want love", r)
	}
	// Love ends after 600 ticks.
	for i := 0; i < int(InLoveTicks); i++ {
		if !a.InLove() {
			t.Fatalf("love ended after %d ticks", i)
		}
		a.Tick(true)
	}
	if a.InLove() {
		t.Fatal("still in love after 600 ticks")
	}
}

// Feeding a baby takes 10% of its remaining time off (AgeableMob.getSpeedUpSecondsWhenFeeding).
func TestFeedingBabyGrowsTenPercent(t *testing.T) {
	var a Animal
	a.SetBaby(true)
	r, _ := a.Feed(true, uuid.New())
	if r != FeedGrow {
		t.Fatalf("baby feed = %v", r)
	}
	// 24000 ticks = 1200 s, 10% = 120 s = 2400 ticks.
	if a.Age() != BabyStartAge+2400 || a.ForcedAge() != 2400 || a.ForcedAgeTimer() != 40 {
		t.Fatalf("age %d forced %d timer %d", a.Age(), a.ForcedAge(), a.ForcedAgeTimer())
	}
	if a.InLove() {
		t.Fatal("baby fell in love")
	}
	// Happy villager particles every 4th tick of the 40 tick timer.
	n := 0
	for i := 0; i < 40; i++ {
		if a.Tick(true).HappyParticle {
			n++
		}
	}
	if n != 10 {
		t.Fatalf("%d happy particles, want 10", n)
	}
}

// A baby growing up through forced growth gets the forced age as breeding cooldown (AgeableMob.ageUp).
func TestForcedGrowthBecomesCooldown(t *testing.T) {
	var a Animal
	a.SetAge(-100)
	if !a.AgeUp(10, true) {
		t.Fatal("no boundary growing up")
	}
	if a.Baby() || a.Age() != 100 {
		t.Fatalf("age %d, want the forced 100", a.Age())
	}
}

// The golden dandelion locks a baby, which then never grows, and unlocks it again.
func TestGoldenDandelionLock(t *testing.T) {
	var a Animal
	a.SetBaby(true)
	if !a.CanUseGoldenDandelion() || !a.ToggleAgeLock() {
		t.Fatal("lock")
	}
	if a.CanUseGoldenDandelion() {
		t.Fatal("no cooldown")
	}
	particles := 0
	for i := 0; i < 30000; i++ {
		if a.Tick(true).AgeLockParticle {
			particles++
		}
	}
	if !a.Baby() || a.Age() != BabyStartAge {
		t.Fatalf("locked baby aged to %d", a.Age())
	}
	if particles != 20 {
		t.Fatalf("%d lock particles, want 20", particles)
	}
	if r, _ := a.Feed(true, uuid.Nil); r != FeedPass {
		t.Fatalf("locked baby fed: %v", r)
	}
	if a.ToggleAgeLock() || a.Age() != BabyStartAge {
		t.Fatal("unlock")
	}
}

func TestNBTRoundTrip(t *testing.T) {
	var a Animal
	a.SetBaby(true)
	a.AgeUp(5, true)
	a.SetInLoveTime(123)
	a.loveCause = uuid.New()
	m := map[string]any{}
	a.EncodeNBT(m)
	var b Animal
	b.DecodeNBT(m)
	if b.Age() != a.Age() || b.ForcedAge() != a.ForcedAge() || b.InLoveTime() != 123 || b.LoveCause() != a.LoveCause() {
		t.Fatalf("round trip: %+v -> %+v", a, b)
	}
}

func TestFoodTags(t *testing.T) {
	if !CowFood.Has(item.Wheat{}) || CowFood.Has(block.Carrot{}) {
		t.Fatal("cow food")
	}
	if !PigFood.Has(block.Carrot{}) || !PigFood.Has(block.Potato{}) || !PigFood.Has(item.Beetroot{}) {
		t.Fatal("pig food")
	}
	if !ChickenFood.Has(block.WheatSeeds{}) || !ChickenFood.Has(block.PumpkinSeeds{}) || !ChickenFood.Has(item.PitcherPod{}) {
		t.Fatal("chicken food")
	}
	if !BeeFood.Has(block.Flower{Type: block.Cornflower()}) || !BeeFood.Has(block.DoubleFlower{Type: block.Sunflower()}) {
		t.Fatal("bee food")
	}
	if CowFood.Has(nil) {
		t.Fatal("nil item")
	}
}
