//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/misc"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestPiglinAngryAtGoldBreak: an adult piglin ignores a player in gold
// armour until the player breaks a gold block nearby (block_sensor ->
// important_block_destroyed_event -> take_target_as_response_to_block_break,
// whose target goal has no gold armour filter); then it targets the player.
func TestPiglinAngryAtGoldBreak(t *testing.T) {
	needs(t, true, true, append(walkers, "minecraft:block_sensor", "minecraft:behavior.nearest_attackable_target")...)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 16, block.Stone{}) })
	pg := w.Spawn("minecraft:piglin", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "spawn_adult"})
	p := w.AddPlayer("p", mgl64.Vec3{4.5, 1, 0.5})
	p.With(func(pl *player.Player) {
		pl.Armour().SetHelmet(item.NewStack(item.Helmet{Tier: item.ArmourTierGold{}}, 1))
	})
	targets := func(tx *world.Tx) bool {
		m, _ := pg.Mob(tx)
		tg, ok := m.Target()
		return ok && tg.H() == p.H()
	}
	if _, ok := w.TickUntil(60, targets); ok {
		t.Fatal("piglin targets a player in gold armour before any block broke")
	}
	w.Do(func(tx *world.Tx) {
		e, _ := p.Entity(tx)
		misc.BlockBroken(tx, cube.Pos{3, 1, 3}, block.Gold{}, e)
	})
	if n, ok := w.TickUntil(60, targets); !ok {
		t.Fatalf("piglin not angry at the gold block breaker after %d ticks:\n%v", n, w.Dump(pg))
	}
}
