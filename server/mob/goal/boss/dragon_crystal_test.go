//go:build crossfly_full

package boss_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	bc "github.com/df-mc/dragonfly/server/mob/component/boss"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestDragonCrystalDestroyed: any end crystal destroyed near the dragon
// sends it after the player nearest the crystal (EnderDragon::
// onCrystalDestroyed); only the healing crystal also hurts its head for 10,
// credited to that player.
func TestDragonCrystalDestroyed(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:boss")
	p := w.AddPlayer("hunter", mgl64.Vec3{18.5, 1, 20.5})
	r := spawnDragon(t, w, mgl64.Vec3{0.5, 10, 0.5})
	var healing, other *world.EntityHandle
	w.Do(func(tx *world.Tx) {
		healing = entity.NewEndCrystal(world.EntitySpawnOpts{Position: mgl64.Vec3{8.5, 1, 0.5}})
		other = entity.NewEndCrystal(world.EntitySpawnOpts{Position: mgl64.Vec3{20.5, 1, 20.5}})
		tx.AddEntity(healing)
		tx.AddEntity(other)
		mobOf(t, tx, r).SetHealth(100)
	})
	if _, ok := w.TickUntil(200, func(tx *world.Tx) bool {
		d := bc.DragonOf(mobOf(t, tx, r))
		return d.CrystalHealing() == healing && d.NumCrystals == 2
	}); !ok {
		t.Fatal("the near crystal never healed the dragon")
	}
	destroy := func(h *world.EntityHandle) (before float64) {
		w.Do(func(tx *world.Tx) {
			m := mobOf(t, tx, r)
			m.ResetTarget()
			before = m.Health()
			e, _ := h.Entity(tx)
			_ = e.(*entity.Ent).Close()
		})
		w.Tick(1)
		return before
	}

	before := destroy(other)
	with(t, w, r, func(tx *world.Tx, m *mob.Mob) {
		pl, _ := p.Entity(tx)
		if m.TargetHandle() != pl.H() {
			t.Errorf("dragon did not go after the player when a crystal broke")
		}
		if m.Health() < before {
			t.Errorf("a crystal that did not heal it hurt the dragon: %v -> %v", before, m.Health())
		}
	})

	before = destroy(healing)
	with(t, w, r, func(tx *world.Tx, m *mob.Mob) {
		pl, _ := p.Entity(tx)
		if got := before - m.Health(); math.Abs(got-10) > 1e-6 {
			t.Errorf("healing crystal broke for %v damage, want 10 to the head", got)
		}
		if by, _, ok := m.LastHurtBy(); !ok || by.H() != pl.H() {
			t.Errorf("the damage was not the player's")
		}
		if m.TargetHandle() != pl.H() {
			t.Errorf("dragon did not go after the player")
		}
	})
}

// TestDragonKilledSaved: that a dragon was killed is kept in the world's
// database (TheEnd, DragonFight.PreviouslyKilled) and survives a restart,
// so later dragons drop 500 experience instead of 12000.
func TestDragonKilledSaved(t *testing.T) {
	w := setup(t, mobtest.Options{Persistent: true}, "minecraft:boss")
	if bc.DragonPreviouslyKilled(w.W) {
		t.Fatal("a new world had a dragon killed")
	}
	bc.SetDragonPreviouslyKilled(w.W)
	w.Reload()
	if !bc.DragonPreviouslyKilled(w.W) {
		t.Errorf("the killed dragon was forgotten after a restart")
	}
}
