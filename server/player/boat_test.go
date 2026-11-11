//go:build crossfly_full

package player

import (
	"context"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

func boatPlayer(tx *world.Tx, name string, pos mgl64.Vec3) *Player {
	h := world.EntitySpawnOpts{Position: pos}.New(Type, Config{
		Name: name, UUID: uuid.New(), GameMode: world.GameModeSurvival, Health: 20, MaxHealth: 20, Position: pos,
	})
	return tx.AddEntity(h).(*Player)
}

func TestBoatPlaceMountDismount(t *testing.T) {
	reg := entity.WithJavaRedstone(entity.DefaultRegistry.Config()).New(append(entity.DefaultRegistry.Types(), entity.JavaRedstoneTypes()...))
	w := world.Config{Synchronous: true, Entities: reg}.New()
	t.Cleanup(func() { _ = w.Close() })
	_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		for x := -3; x <= 3; x++ {
			for z := -3; z <= 3; z++ {
				tx.SetBlock(cube.Pos{x, 59, z}, block.Stone{}, nil)
			}
		}
		a := boatPlayer(tx, "Alice", mgl64.Vec3{2.5, 60, 0.5})
		b := boatPlayer(tx, "Bob", mgl64.Vec3{-2.5, 60, 0.5})

		// Placing: on the top face of a block with air above, one item used up.
		a.SetHeldItems(item.NewStack(item.Boat{Wood: item.BoatBirch}, 1), item.Stack{})
		ctx := &item.UseContext{}
		if (item.Boat{Wood: item.BoatBirch}).UseOnBlock(cube.Pos{0, 59, 0}, cube.FaceNorth, mgl64.Vec3{0.5, 0.5, 0}, tx, a, ctx) {
			t.Fatal("placed on a side face")
		}
		if !(item.Boat{Wood: item.BoatBirch}).UseOnBlock(cube.Pos{0, 59, 0}, cube.FaceUp, mgl64.Vec3{0.5, 1, 0.5}, tx, a, ctx) || ctx.CountSub != 1 {
			t.Fatal("not placed on top")
		}
		var boat *entity.Boat
		for e := range tx.Entities() {
			if bt, ok := e.(*entity.Boat); ok {
				boat = bt
			}
		}
		if boat == nil || boat.Wood() != item.BoatBirch || boat.Position() != (mgl64.Vec3{0.5, 60, 0.5}) {
			t.Fatalf("boat %v", boat)
		}
		if yaw := boat.Rotation().Yaw(); yaw != a.Rotation().Yaw()+90 {
			t.Fatalf("boat yaw %v", yaw)
		}

		// Two riders: the first drives, they sit front and back.
		seat, ok := boat.NextFreeSeatIndex(mgl64.Vec3{})
		a.MountEntity(tx, boat, seat)
		seat, ok = boat.NextFreeSeatIndex(mgl64.Vec3{})
		if !ok {
			t.Fatal("no second seat")
		}
		b.MountEntity(tx, boat, seat)
		if len(boat.Riders()) != 2 || boat.ControllingRider() != a.H() {
			t.Fatalf("riders %v", boat.Riders())
		}
		if off, _ := a.SeatOffset(); off[0] != 0.2 {
			t.Fatalf("driver seat %v", off)
		}
		if off, _ := b.SeatOffset(); off[0] != -0.6 {
			t.Fatalf("passenger seat %v", off)
		}
		if _, ok := boat.NextFreeSeatIndex(mgl64.Vec3{}); ok {
			t.Fatal("third seat")
		}

		// The driver gets off: the passenger takes the front seat and drives.
		a.DismountEntity(tx)
		if a.RidingEntityHandle() != nil || boat.ControllingRider() != b.H() || b.SeatIndex() != 0 {
			t.Fatalf("after dismount: controller %v seat %d", boat.ControllingRider(), b.SeatIndex())
		}
		if off, _ := b.SeatOffset(); off[0] != 0 {
			t.Fatalf("lone rider seat %v", off)
		}

		// A sneaking player cannot get on.
		a.StartSneaking()
		if boat.AcceptsRider(a) {
			t.Fatal("sneaking player accepted")
		}
		a.StopSneaking()

		// Breaking the boat drops its riders.
		for i := 0; i < 4; i++ {
			entity.HurtEntity(boat, 1, entity.AttackDamageSource{Attacker: a})
		}
		if b.RidingEntityHandle() != nil {
			t.Fatal("rider kept on a broken boat")
		}
		return struct{}{}, nil
	})
}
