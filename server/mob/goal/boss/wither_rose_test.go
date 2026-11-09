//go:build crossfly_full

package boss_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	bc "github.com/df-mc/dragonfly/server/mob/component/boss"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// roseAt reports whether a wither rose grows at p, and counts the wither
// rose items lying around.
func roseAt(tx *world.Tx, p cube.Pos) (planted bool, items int) {
	if f, ok := tx.Block(p).(block.Flower); ok && f.Type == block.WitherRose() {
		planted = true
	}
	for e := range tx.Entities() {
		if it, ok := e.(*entity.Ent); ok {
			if b, ok := it.Behaviour().(*entity.ItemBehaviour); ok {
				if f, ok := b.Item().Item().(block.Flower); ok && f.Type == block.WitherRose() {
					items++
				}
			}
		}
	}
	return
}

// TestWitherRose: a mob the wither kills leaves a wither rose where it died
// (Actor::_tryPlantWitherRose) on soil, a wither rose item where no rose can
// stand, and nothing when something else killed it.
func TestWitherRose(t *testing.T) {
	for _, tc := range []struct {
		name          string
		soil          world.Block
		byWither      bool
		planted, item bool
	}{
		{"grass", block.Grass{}, true, true, false},
		{"stone", block.Stone{}, true, false, true},
		{"not the wither", block.Grass{}, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := setup(t, mobtest.Options{}, "minecraft:boss")
			wr := w.Spawn(bc.WitherID, mgl64.Vec3{8.5, 1, 8.5})
			zr := w.Spawn("minecraft:zombie", mgl64.Vec3{8.5, 1, -8.5})
			pr := w.Spawn("minecraft:pig", mgl64.Vec3{0.5, 1, 0.5})
			w.Tick(1)
			w.Do(func(tx *world.Tx) {
				tx.SetBlock(cube.Pos{0, 0, 0}, tc.soil, nil)
				killer := mobOf(t, tx, zr)
				if tc.byWither {
					killer = mobOf(t, tx, wr)
				}
				mobOf(t, tx, pr).Hurt(1000, entity.AttackDamageSource{Attacker: killer})
				planted, items := roseAt(tx, cube.Pos{0, 1, 0})
				if planted != tc.planted || (items > 0) != tc.item {
					t.Errorf("rose planted %v, items %d; want planted %v, item %v", planted, items, tc.planted, tc.item)
				}
			})
		})
	}
}

// TestWitherRosePlayer: a player killed by a wither's explosion leaves a
// wither rose too.
func TestWitherRosePlayer(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:boss")
	wr := w.Spawn(bc.WitherID, mgl64.Vec3{8.5, 1, 8.5})
	p := w.AddPlayer("victim", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(1)
	w.Do(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 0, 0}, block.Dirt{}, nil)
		bc.Explode(tx, mobOf(t, tx, wr), mgl64.Vec3{0.5, 1.5, 0.5}, 7, false, false)
		if planted, _ := roseAt(tx, cube.Pos{0, 1, 0}); !planted {
			t.Errorf("no wither rose where the player died (health %v)", p.HealthTx(tx))
		}
	})
}
