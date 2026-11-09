//go:build crossfly_full

// Package mobsoff checks that Crossfly Full with [Mobs] Behaviour = "off"
// pays nothing for the mobs: the mob packages are linked (as in the server
// binary) but vanilla.Register is not called.
package mobsoff_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	_ "github.com/df-mc/dragonfly/server/mob/vanilla" // links every mob package, as Crossfly Full does
	"github.com/df-mc/dragonfly/server/world"
)

// TestMobsOffCostNothing: linking the mob packages registers no world tick
// hook (natural spawning and villages hook in only from vanilla.Register),
// no entity type (so no mob is ever ticked, loaded or shown) and leaves the
// mobs off for players and sessions.
func TestMobsOffCostNothing(t *testing.T) {
	if n := world.TickHooks(); n != 0 {
		t.Errorf("%d world tick hooks registered with the mobs off, want 0", n)
	}
	if n := len(mob.Types()); n != 0 {
		t.Errorf("%d mob types registered with the mobs off, want 0", n)
	}
	if entity.Mobs() {
		t.Error("entity.Mobs() is true with the mobs off")
	}
}
