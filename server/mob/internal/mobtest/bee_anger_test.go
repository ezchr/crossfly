//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestBeeSwarmAngers: hitting one bee angers the bees around it (bee.json minecraft:angry uses the camelCase
// broadcastAnger and broadcastRange keys AngryDefinition accepts).
func TestBeeSwarmAngers(t *testing.T) {
	mobtest.RequireDefinitions(t)
	mobtest.RequireFilters(t)
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	w.Do(func(tx *world.Tx) { w.Arena(tx, 12, 4) })
	var bees []*mobtest.Ref
	for i := range 4 {
		bees = append(bees, w.Spawn("minecraft:bee", mgl64.Vec3{float64(i)*2 - 3 + 0.5, 2, 4.5}, mob.SpawnOptions{Event: "minecraft:spawn_adult"}))
	}
	p := w.AddPlayer("beekeeper", mgl64.Vec3{0.5, 1, -4.5})
	w.Tick(2)
	w.Do(func(tx *world.Tx) {
		m, _ := bees[0].Mob(tx)
		for e := range tx.Entities() {
			if pl, ok := e.(*player.Player); ok {
				m.Hurt(1, entity.AttackDamageSource{Attacker: pl})
			}
		}
	})
	n, ok := w.TickUntil(40, func(tx *world.Tx) bool {
		for _, b := range bees {
			m, ok := b.Mob(tx)
			if !ok {
				return false
			}
			tg, has := m.Target()
			if !has || tg.H() != p.H() || !m.Meta().Angry {
				return false
			}
		}
		return true
	})
	if !ok {
		for i, b := range bees {
			t.Errorf("bee %d not angry at the player:\n%v", i, w.Dump(b))
		}
		return
	}
	t.Logf("swarm angry after %d ticks", n)
}
