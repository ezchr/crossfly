//go:build crossfly_full

package animal_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// noGriefing turns mob griefing off for the test.
func noGriefing(t *testing.T) {
	old := combat.MobGriefing
	combat.MobGriefing = func(*world.Tx) bool { return false }
	t.Cleanup(func() { combat.MobGriefing = old })
}

// TestSilverfishStrolls: SilverfishMergeWithStoneGoal is a random stroll
// when the silverfish does not hide (here: mob griefing off).
func TestSilverfishStrolls(t *testing.T) {
	noGriefing(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Arena(tx, 12, 2) })
	s := w.Spawn("minecraft:silverfish", mgl64.Vec3{0.5, 1, 0.5})
	install(t, w, s, "minecraft:behavior.silverfish_merge_with_stone", `{}`)
	start := s.Pos()
	n, ok := w.TickUntil(2000, func(tx *world.Tx) bool {
		p := s.PosTx(tx)
		return mgl64.Vec2{p[0] - start[0], p[2] - start[2]}.Len() > 2
	})
	if !ok {
		t.Fatalf("silverfish never wandered:\n%v", w.Dump(s))
	}
	t.Logf("wandered off after %d ticks to %v", n, s.Pos())
}

func silverfishCount(tx *world.Tx) (n int, persistent bool, last mgl64.Vec3) {
	persistent = true
	for e := range tx.Entities() {
		if m, ok := mob.Of(e); ok && m.Identifier() == "minecraft:silverfish" {
			n++
			persistent = persistent && m.Persistent()
			last = m.Position()
		}
	}
	return n, persistent, last
}

// TestInfestedBlockReleasesSilverfish: InfestedBlock::spawnAfterBreak lets
// out a persistent silverfish at the block's position, but not when broken
// with silk touch or in creative.
func TestInfestedBlockReleasesSilverfish(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	pos := cube.Pos{2, 1, 0}
	w.Do(func(tx *world.Tx) { w.Arena(tx, 6, 0) })
	p := w.AddPlayer("miner", mgl64.Vec3{0.5, 1, 0.5})
	breakIt := func(b world.Block) (int, bool, mgl64.Vec3) {
		var n int
		var pers bool
		var at mgl64.Vec3
		w.Do(func(tx *world.Tx) { tx.SetBlock(pos, b, nil) })
		p.With(func(pl *player.Player) {
			pl.BreakBlock(pos)
			if _, air := pl.Tx().Block(pos).(block.Air); !air {
				t.Fatalf("%T not broken", b)
			}
			n, pers, at = silverfishCount(pl.Tx())
		})
		return n, pers, at
	}
	silk := item.NewStack(item.Pickaxe{Tier: item.ToolTierDiamond}, 1).WithEnchantments(item.NewEnchantment(enchantment.SilkTouch, 1))
	p.Hold(silk)
	if n, _, _ := breakIt(block.InfestedStone{}); n != 0 {
		t.Fatalf("silk touch: %d silverfish", n)
	}
	p.Hold(item.NewStack(item.Pickaxe{Tier: item.ToolTierDiamond}, 1))
	n, pers, at := breakIt(block.InfestedStoneBricks{})
	if n != 1 || !pers {
		t.Fatalf("pickaxe: %d silverfish (persistent %v), want 1 persistent", n, pers)
	}
	if at != pos.Vec3() {
		t.Errorf("silverfish at %v, want the block position %v", at, pos.Vec3())
	}
	p.SetGameMode(world.GameModeCreative)
	if n, _, _ := breakIt(block.InfestedCobblestone{}); n != 1 {
		t.Fatalf("creative: %d silverfish, want the 1 from before", n)
	}
}

// TestSilverfishWakeUpFriendsBreaksBlocks: with mob griefing the woken
// blocks break (Level::destroyBlock) and release persistent silverfish;
// without it they turn back into their host blocks.
func TestSilverfishWakeUpFriendsBreaksBlocks(t *testing.T) {
	for _, grief := range []bool{true, false} {
		if !grief {
			noGriefing(t)
		}
		w := mobtest.New(t, mobtest.Options{})
		pos := cube.Pos{0, 1, 3}
		w.Do(func(tx *world.Tx) {
			w.Arena(tx, 12, 0)
			tx.SetBlock(pos, block.InfestedStone{}, nil)
		})
		s := w.Spawn("minecraft:silverfish", mgl64.Vec3{0.5, 1, 0.5})
		install(t, w, s, "minecraft:behavior.silverfish_wake_up_friends", `{}`)
		p := w.AddPlayer("attacker", mgl64.Vec3{-3.5, 1, 0.5})
		p.With(func(pl *player.Player) {
			m, _ := s.Mob(pl.Tx())
			m.Hurt(1, entity.AttackDamageSource{Attacker: pl})
		})
		w.Tick(25)
		w.Do(func(tx *world.Tx) {
			n, _, _ := silverfishCount(tx)
			b := tx.Block(pos)
			if grief {
				if _, air := b.(block.Air); !air || n != 2 {
					t.Errorf("griefing: block %T, %d silverfish; want air and 2", b, n)
				}
				for e := range tx.Entities() {
					if m, ok := mob.Of(e); ok && m.H() != s.H() && !m.Persistent() {
						t.Error("released silverfish not persistent")
					}
				}
				return
			}
			if name, _ := b.EncodeBlock(); name != "minecraft:stone" || n != 1 {
				t.Errorf("no griefing: block %v, %d silverfish; want stone and 1", name, n)
			}
		})
	}
}
