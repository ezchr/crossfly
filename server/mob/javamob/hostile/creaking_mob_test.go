//go:build crossfly_full

package hostile

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestCreakingFreezesWhenWatched: a survival player looking at a creaking
// within 12 blocks activates it and keeps it frozen; once the player looks
// away it walks up and hits.
func TestCreakingFreezesWhenWatched(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Midnight})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 16, block.Stone{}) })
	c := spawnJ(t, w, creakingID, mgl64.Vec3{0.5, 1, 6.5})
	p := w.AddPlayer("tester", mgl64.Vec3{0.5, 1, 0.5}) // yaw 0 looks south (+z), at the creaking
	w.Tick(40)
	start := positionOf(t, w, c)
	withJ(t, w, c, func(m *javamob.Mob) {
		cr := creakingOf(m.Mob)
		if !cr.Active() || cr.CanMove() {
			t.Fatalf("watched creaking: active %v canMove %v, want active and frozen", cr.Active(), cr.CanMove())
		}
	})
	w.Tick(40)
	if d := positionOf(t, w, c).Sub(start).Len(); d > 0.05 {
		t.Errorf("watched creaking moved %.2f blocks", d)
	}
	p.With(func(pl *player.Player) { pl.Move(mgl64.Vec3{}, 180, 0) })
	hurt := false
	for i := 0; i < 200 && !hurt; i++ {
		w.Tick(1)
		p.With(func(pl *player.Player) { hurt = pl.Health() < pl.MaxHealth() })
	}
	if !hurt {
		t.Errorf("unwatched creaking did not reach and hit the player (moved %.2f)", positionOf(t, w, c).Sub(start).Len())
	}
}
