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
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// useJukebox makes p use the jukebox at pos holding held (a disc puts it in, anything with a disc
// inside takes it out).
func useJukebox(t *testing.T, w *mobtest.World, p *mobtest.Player, pos cube.Pos, held item.Stack) {
	t.Helper()
	w.Do(func(tx *world.Tx) {
		e, _ := p.Entity(tx)
		pl := e.(*player.Player)
		pl.SetHeldItems(held, item.Stack{})
		j, ok := tx.Block(pos).(block.Jukebox)
		if !ok {
			t.Fatalf("no jukebox at %v", pos)
		}
		j.Activate(pos, cube.FaceUp, tx, pl, &item.UseContext{})
	})
}

func dancing(t *testing.T, w *mobtest.World, r *mobtest.Ref) bool {
	t.Helper()
	v := false
	w.Do(func(tx *world.Tx) { v = social.Dancing(mobOf(t, tx, r)) })
	return v
}

// hold keeps the mobs at their places, since they fly about.
func hold(t *testing.T, tx *world.Tx, at map[*mobtest.Ref]mgl64.Vec3) {
	for r, pos := range at {
		mobOf(t, tx, r).Teleport(pos)
	}
}

// TestDanceAtJukebox: a playing jukebox makes the allays within 10 blocks and the parrots within
// 3.46 blocks dance (DanceComponentListener); taking the disc out stops them, and so does moving
// away (DanceSystem). A mob that comes close while the music plays starts within a second.
func TestDanceAtJukebox(t *testing.T) {
	w := setup(t, mobtest.Options{Time: mobtest.Noon})
	jb := cube.Pos{0, 1, 0}
	w.Do(func(tx *world.Tx) { tx.SetBlock(jb, block.Jukebox{}, nil) })
	p := w.AddPlayer("p", mgl64.Vec3{0.5, 1, 3.5})
	allay := w.Spawn("minecraft:allay", mgl64.Vec3{7.5, 2, 0.5})
	parrot := w.Spawn("minecraft:parrot", mgl64.Vec3{2.5, 1, 0.5})
	farParrot := w.Spawn("minecraft:parrot", mgl64.Vec3{-5.5, 1, 0.5})
	far := map[*mobtest.Ref]mgl64.Vec3{farParrot: {-5.5, 1, 0.5}}
	w.Tick(2)

	useJukebox(t, w, p, jb, item.NewStack(item.MusicDisc{DiscType: sound.DiscCat()}, 1))
	if !dancing(t, w, allay) || !dancing(t, w, parrot) {
		t.Fatalf("allay 7 blocks and parrot 2 blocks away do not dance: %v %v", dancing(t, w, allay), dancing(t, w, parrot))
	}
	if dancing(t, w, farParrot) {
		t.Fatal("parrot 6 blocks away dances")
	}

	// The far parrot comes close: it hears the next record.playing within a second.
	far[farParrot] = mgl64.Vec3{-1.5, 1, 0.5}
	if _, ok := w.TickUntil(25, func(tx *world.Tx) bool {
		hold(t, tx, far)
		return social.Dancing(mobOf(t, tx, farParrot))
	}); !ok {
		t.Fatal("parrot that came next to the playing jukebox did not start dancing")
	}

	// Moving away stops the allay at once.
	w.Do(func(tx *world.Tx) { mobOf(t, tx, allay).Teleport(mgl64.Vec3{12.5, 2, 0.5}) })
	w.Tick(1)
	if dancing(t, w, allay) {
		t.Error("allay 12 blocks from the jukebox still dances")
	}

	// Taking the disc out stops the parrots.
	w.Do(func(tx *world.Tx) {
		hold(t, tx, far)
		mobOf(t, tx, parrot).Teleport(mgl64.Vec3{2.5, 1, 0.5})
	})
	useJukebox(t, w, p, jb, item.Stack{})
	if dancing(t, w, parrot) || dancing(t, w, farParrot) {
		t.Fatal("parrots dance after the disc was taken out")
	}
}

// TestDanceStopsWhenSongEnds: the jukebox plays a disc for its duration (creator music box: 73 s,
// RecordItem::RecordItem) and the dancing stops when the song is over.
func TestDanceStopsWhenSongEnds(t *testing.T) {
	w := setup(t, mobtest.Options{Time: mobtest.Noon})
	jb := cube.Pos{0, 1, 0}
	w.Do(func(tx *world.Tx) { tx.SetBlock(jb, block.Jukebox{}, nil) })
	p := w.AddPlayer("p", mgl64.Vec3{0.5, 1, 3.5})
	allay := w.Spawn("minecraft:allay", mgl64.Vec3{3.5, 2, 0.5})
	at := map[*mobtest.Ref]mgl64.Vec3{allay: {3.5, 2, 0.5}}
	useJukebox(t, w, p, jb, item.NewStack(item.MusicDisc{DiscType: sound.DiscCreatorMusicBox()}, 1))
	stopped := 0
	w.Each(1520, func(tx *world.Tx, tick int) bool {
		hold(t, tx, at)
		if !social.Dancing(mobOf(t, tx, allay)) {
			stopped = tick
			return false
		}
		return true
	})
	t.Logf("allay stopped dancing after %d ticks", stopped)
	if stopped < 1440 || stopped > 1500 {
		t.Fatalf("allay stopped dancing after %d ticks, want when the 73 s song ends (1460-1480)", stopped)
	}
	// The disc stays in, silent: no more dancing.
	w.Tick(40)
	if dancing(t, w, allay) {
		t.Error("allay dances to a finished disc")
	}
}
