//go:build crossfly_full

package social_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/social"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// liked returns the note block the allay behind a likes, if any.
func liked(t *testing.T, w *mobtest.World, a *mobtest.Ref) (cube.Pos, bool) {
	t.Helper()
	var (
		p  cube.Pos
		ok bool
	)
	w.Do(func(tx *world.Tx) { p, ok = social.LikedNoteBlock(mobOf(t, tx, a), 600) })
	return p, ok
}

// TestNoteBlockRegistersWithAllay: a note block played by a player and one
// played by redstone both reach the allays within 16 blocks through
// block.NoteListener, and only those.
func TestNoteBlockRegistersWithAllay(t *testing.T) {
	w := setup(t, mobtest.Options{Time: mobtest.Noon}, "minecraft:behavior.stay_near_noteblock")
	p := w.AddPlayer("p", mgl64.Vec3{0.5, 1, 4.5})
	byHand, byPower := cube.Pos{-10, 1, 0}, cube.Pos{10, 1, 0}
	west := w.Spawn("minecraft:allay", mgl64.Vec3{-12.5, 2, 0.5}) // 22 blocks from byPower
	east := w.Spawn("minecraft:allay", mgl64.Vec3{12.5, 2, 0.5})  // 22 blocks from byHand
	w.Do(func(tx *world.Tx) {
		tx.SetBlock(byHand, block.Note{}, nil)
		tx.SetBlock(byPower, block.Note{}, nil)
		e, _ := p.Entity(tx)
		block.Note{}.Activate(byHand, cube.FaceUp, tx, e.(*player.Player), &item.UseContext{})
	})
	if got, ok := liked(t, w, west); !ok || got != byHand {
		t.Fatalf("allay likes %v (%v) after a player played the note block, want %v", got, ok, byHand)
	}
	if got, ok := liked(t, w, east); ok {
		t.Errorf("allay 22 blocks from the played note block likes %v", got)
	}
	w.Do(func(tx *world.Tx) {
		block.Note{}.RedstonePowerPostUpdate(byPower, tx, block.Note{}, block.Note{Powered: true}, 0, 15)
	})
	if got, ok := liked(t, w, east); !ok || got != byPower {
		t.Fatalf("allay likes %v (%v) after redstone played a note block, want %v", got, ok, byPower)
	}
	if got, _ := liked(t, w, west); got != byHand {
		t.Errorf("far allay switched to %v", got)
	}
}

// TestAllayKeepsItsNoteBlock: within 600 ticks of the last note it heard an
// allay ignores other note blocks, but the same one renews it
// (AllayVibrationConfig::onSignalReceive).
func TestAllayKeepsItsNoteBlock(t *testing.T) {
	w := setup(t, mobtest.Options{Time: mobtest.Noon}, "minecraft:behavior.stay_near_noteblock")
	a := w.Spawn("minecraft:allay", mgl64.Vec3{2.5, 2, 0.5})
	first, second := cube.Pos{0, 1, 0}, cube.Pos{4, 1, 0}
	w.Do(func(tx *world.Tx) {
		tx.SetBlock(first, block.Note{}, nil)
		tx.SetBlock(second, block.Note{}, nil)
		social.NoteBlockPlayed(tx, first)
	})
	w.Tick(300)
	w.Do(func(tx *world.Tx) {
		// Back between the note blocks: it may have hovered away.
		mobOf(t, tx, a).Teleport(mgl64.Vec3{2.5, 2, 0.5})
		social.NoteBlockPlayed(tx, second)
	})
	if got, _ := liked(t, w, a); got != first {
		t.Fatalf("allay switched to %v 300 ticks after hearing %v", got, first)
	}
	w.Do(func(tx *world.Tx) { social.NoteBlockPlayed(tx, first) })
	w.Tick(500)
	w.Do(func(tx *world.Tx) {
		// Back between the note blocks: it may have hovered away.
		mobOf(t, tx, a).Teleport(mgl64.Vec3{2.5, 2, 0.5})
		social.NoteBlockPlayed(tx, second)
	})
	if got, _ := liked(t, w, a); got != first {
		t.Fatalf("allay switched to %v although %v played again 500 ticks ago", got, first)
	}
	w.Tick(101)
	w.Do(func(tx *world.Tx) {
		// Back between the note blocks: it may have hovered away.
		mobOf(t, tx, a).Teleport(mgl64.Vec3{2.5, 2, 0.5})
		social.NoteBlockPlayed(tx, second)
	})
	if got, ok := liked(t, w, a); !ok || got != second {
		t.Fatalf("allay likes %v (%v) 601 ticks after the last note, want %v", got, ok, second)
	}
}
