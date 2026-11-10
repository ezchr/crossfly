//go:build crossfly_full

package hostile

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestCreakingCanMove: Creaking.checkCanMove: a watcher freezes it and
// activates it within 12 blocks; a disguised watcher (carved pumpkin) does
// not freeze an active creaking; with no attackable player it deactivates.
func TestCreakingCanMove(t *testing.T) {
	h := &world.EntityHandle{}
	c := &Creaking{canMove: true}
	if move, act, deact := c.checkCanMove(nil); !move || act != nil || deact {
		t.Errorf("no players: %v %v %v", move, act, deact)
	}
	far := creakingWatcher{attackable: true, looking: true, distSq: 13 * 13, h: h}
	if move, act, _ := c.checkCanMove([]creakingWatcher{far}); !move || act != nil {
		t.Errorf("a far watcher does not activate an idle creaking: move %v act %v", move, act)
	}
	near := creakingWatcher{attackable: true, looking: true, distSq: 11 * 11, h: h}
	move, act, _ := c.checkCanMove([]creakingWatcher{near})
	if move || act != h {
		t.Fatalf("near watcher: move %v activate %v", move, act)
	}
	c.active = true
	if move, _, _ := c.checkCanMove([]creakingWatcher{far}); move {
		t.Errorf("an active creaking freezes under any look")
	}
	disguised := near
	disguised.disguised = true
	if move, _, deact := c.checkCanMove([]creakingWatcher{disguised}); !move || deact {
		t.Errorf("pumpkin head: move %v deactivate %v", move, deact)
	}
	away := near
	away.looking = false
	if move, _, deact := c.checkCanMove([]creakingWatcher{away}); !move || deact {
		t.Errorf("not looking: move %v deactivate %v", move, deact)
	}
	creative := near
	creative.attackable = false
	if _, _, deact := c.checkCanMove([]creakingWatcher{creative}); !deact {
		t.Errorf("no attackable player left: an active creaking deactivates")
	}
	if s := (&Creaking{active: true, canMove: false}).creakingState(); s != "hostile_observed" {
		t.Errorf("state %v", s)
	}
	if s := (&Creaking{tearingDown: true}).creakingState(); s != "twitching" {
		t.Errorf("state %v", s)
	}
}

// TestCreakingLookingAt: isLookingAtMe with cone 0.5 (60°) at the three gaze
// heights.
func TestCreakingLookingAt(t *testing.T) {
	clear := func(mgl64.Vec3) bool { return true }
	pos := mgl64.Vec3{0, 64, 10}
	heights := creakingGazeHeights(64, 64+2.3, 1)
	eye := mgl64.Vec3{0, 65.62, 0}
	if !lookingAt(pos, viewVector(0, 0), eye, 0.5, heights, clear) {
		t.Errorf("looking straight at it (yaw 0 faces +z)")
	}
	if lookingAt(pos, viewVector(180, 0), eye, 0.5, heights, clear) {
		t.Errorf("looking away")
	}
	if lookingAt(pos, viewVector(70, 0), eye, 0.5, heights, clear) {
		t.Errorf("70 degrees off is outside the cone")
	}
	if !lookingAt(pos, viewVector(55, 0), eye, 0.5, heights, clear) {
		t.Errorf("55 degrees off is inside the cone")
	}
	if lookingAt(pos, viewVector(0, 0), eye, 0.5, heights, func(mgl64.Vec3) bool { return false }) {
		t.Errorf("a wall in between")
	}
}

// TestCreakingStuck: a player's eyes in the creaking for 5 checks.
func TestCreakingStuck(t *testing.T) {
	c := &Creaking{}
	box := cube.Box(-0.45, 0, -0.45, 0.45, 2.7, 0.45)
	in := []mgl64.Vec3{{0, 1.6, 0}}
	for i := 1; i <= 4; i++ {
		if c.creakingPlayerStuck(box, in) {
			t.Fatalf("stuck after %d checks", i)
		}
	}
	if !c.creakingPlayerStuck(box, in) {
		t.Errorf("not stuck after 5 checks")
	}
	if c.creakingPlayerStuck(box, []mgl64.Vec3{{3, 1.6, 0}}) || c.playerStuck != 0 {
		t.Errorf("counter not reset")
	}
}
