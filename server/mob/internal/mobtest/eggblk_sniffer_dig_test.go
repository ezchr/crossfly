//go:build crossfly_full

package mobtest_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestSnifferDigsUpSeeds: a sniffer that found a scent (on_scenting_success) digs and digs up torchflower seeds
// or a pitcher pod (sniffer_seeds.json), at its feet 2.25 blocks ahead (spawn_item_pos_offset).
func TestSnifferDigsUpSeeds(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 24, block.Dirt{}) })
	s := w.Spawn("minecraft:sniffer", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_adult"})
	s.With(func(m *mob.Mob) { m.Fire("on_scenting_success", nil) })
	var seed item.Stack
	var seedPos, snifferPos mgl64.Vec3
	if _, ok := w.TickUntil(1200, func(tx *world.Tx) bool {
		for e := range tx.Entities() {
			ent, ok := e.(*entity.Ent)
			if !ok {
				continue
			}
			if b, ok := ent.Behaviour().(*entity.ItemBehaviour); ok {
				seed, seedPos, snifferPos = b.Item(), ent.Position(), s.PosTx(tx)
				return true
			}
		}
		return false
	}); !ok {
		t.Fatalf("the sniffer dug up nothing: %v", w.Dump(s))
	}
	switch seed.Item().(type) {
	case item.TorchflowerSeeds, item.PitcherPod:
	default:
		t.Errorf("the sniffer dug up %v, want torchflower seeds or a pitcher pod", seed)
	}
	if d := math.Hypot(seedPos[0]-snifferPos[0], seedPos[2]-snifferPos[2]); math.Abs(d-2.25) > 0.3 {
		t.Errorf("the seeds came up %.2f blocks from the sniffer, want 2.25", d)
	}
	if dy := seedPos[1] - snifferPos[1]; dy < -0.1 || dy > 0.4 {
		t.Errorf("the seeds came up %.2f above the sniffer's feet, want about 0", dy)
	}
}
