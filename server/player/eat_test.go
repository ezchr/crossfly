package player

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// TestEatFinishWithinGrace checks that a Bedrock "finished eating" arriving slightly before the
// server's own consume duration is accepted, and that one far too early stops eating without
// consuming, so the next tap starts a new bite instead of eating at once.
func TestEatFinishWithinGrace(t *testing.T) {
	spearWorld(t, world.GameModeSurvival, func(tx *world.Tx, p *Player) {
		p.SetFood(10)
		p.SetHeldItems(item.NewStack(item.Apple{}, 3), item.Stack{})

		p.UseItem() // start
		if !p.usingItem {
			t.Fatalf("not eating after start")
		}
		p.usingSince = time.Now().Add(-1450 * time.Millisecond) // 29 ticks: jitter on a full bite
		p.UseItem()                                              // finish
		if held, _ := p.HeldItems(); held.Count() != 2 || p.Food() != 14 {
			t.Fatalf("count %v food %v after finishing within grace, want 2, 14", held.Count(), p.Food())
		}
		if p.usingItem {
			t.Fatalf("still eating after consuming")
		}

		p.UseItem() // start
		p.usingSince = time.Now().Add(-500 * time.Millisecond)
		p.UseItem() // far too early
		if held, _ := p.HeldItems(); held.Count() != 2 || p.Food() != 14 {
			t.Fatalf("count %v food %v after early finish, want unchanged", held.Count(), p.Food())
		}
		if p.usingItem {
			t.Fatalf("still eating after a refused finish: the next tap would eat at once")
		}
		p.UseItem() // a fresh start, not a finish
		if held, _ := p.HeldItems(); held.Count() != 2 || !p.usingItem {
			t.Fatalf("tap after refused finish: count %v using %v, want 2, true", held.Count(), p.usingItem)
		}
	})
}
