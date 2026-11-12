package item_test

import (
	"context"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"testing"
)

func TestCrossbowArrowDoesNotOneShotUnarmouredPlayer(t *testing.T) {
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
	defer w.Close()
	var target *world.EntityHandle
	w.Do(func(tx *world.Tx) {
		shooter := tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{0, 64, 0}}.New(player.Type, player.Config{Name: "shooter", UUID: uuid.New(), GameMode: world.GameModeSurvival})).(*player.Player)
		target = world.EntitySpawnOpts{Position: mgl64.Vec3{0, 64, 3}}.New(player.Type, player.Config{Name: "target", UUID: uuid.New(), GameMode: world.GameModeSurvival, Health: 20, MaxHealth: 20})
		tx.AddEntity(target)
		loaded := item.Crossbow{Item: item.NewStack(item.Arrow{}, 1)}
		shooter.SetHeldItems(item.NewStack(loaded, 1), item.Stack{})
		if !loaded.ReleaseCharge(shooter, tx, &item.UseContext{}) {
			t.Fatal("loaded crossbow did not fire")
		}
	}).Wait(context.Background())
	for i := 0; i < 3; i++ {
		w.AdvanceTick()
	}
	w.Do(func(tx *world.Tx) {
		e, ok := target.Entity(tx)
		if !ok {
			t.Fatal("target removed")
		}
		health := e.(*player.Player).Health()
		if health >= 20 {
			t.Fatal("test arrow did not hit")
		}
		if health < 8 {
			t.Fatalf("crossbow dealt excessive damage: health=%v", health)
		}
	}).Wait(context.Background())
}
