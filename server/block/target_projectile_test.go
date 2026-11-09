//go:build crossfly_full

package block_test

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"testing"
)

func TestMovingArrowActivatesTargetAtImpact(t *testing.T) {
	w := world.Config{Synchronous: true, SettleSynchronousTransactions: true, Entities: entity.DefaultRegistry}.New()
	defer w.Close()
	at := cube.Pos{3, 64, 0}
	runDispenserWorld(t, w, func(tx *world.Tx) {
		tx.SetBlock(at, block.Target{}, nil)
		tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{0, 64.5, .5}, Velocity: mgl64.Vec3{2, 0, 0}}.New(entity.ArrowType, entity.ProjectileBehaviourConfig{SurviveBlockCollision: true, Damage: 0}))
	})
	for range 3 {
		w.AdvanceTick()
	}
	runDispenserWorld(t, w, func(tx *world.Tx) {
		target := tx.Block(at).(block.Target)
		if target.Power != 15 {
			t.Fatalf("real arrow impact gave %d, want 15", target.Power)
		}
	})
	for range 18 {
		w.AdvanceTick()
	}
	runDispenserWorld(t, w, func(tx *world.Tx) {
		if tx.Block(at).(block.Target).Power != 15 {
			t.Fatal("impact pulse expired before twenty game ticks")
		}
	})
	w.AdvanceTick()
	runDispenserWorld(t, w, func(tx *world.Tx) {
		if tx.Block(at).(block.Target).Power != 0 {
			t.Fatal("impact pulse did not expire after twenty game ticks")
		}
	})
}
