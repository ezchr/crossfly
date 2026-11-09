//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"testing"
)

func TestTargetStrengthUsesHitFaceAndNegativeCoordinates(t *testing.T) {
	for _, test := range []struct {
		face cube.Face
		hit  mgl64.Vec3
		want int
	}{
		{cube.FaceWest, mgl64.Vec3{0, .5, .5}, 15},
		{cube.FaceWest, mgl64.Vec3{0, 0, .5}, 1},
		{cube.FaceWest, mgl64.Vec3{0, .25, .5}, 8},
		{cube.FaceUp, mgl64.Vec3{.5, 1, .5}, 15},
		{cube.FaceNorth, mgl64.Vec3{.5, .5, 0}, 15},
		{cube.FaceWest, mgl64.Vec3{-3, -2.5, -1.5}, 15},
		{cube.FaceDown, mgl64.Vec3{-.75, -4, -.5}, 8},
	} {
		if got := targetStrength(test.face, test.hit); got != test.want {
			t.Errorf("%s %v: strength %d, want %d", test.face, test.hit, got, test.want)
		}
	}
}

func TestTargetPulseDurationAndRetrigger(t *testing.T) {
	for _, arrow := range []bool{false, true} {
		w := world.Config{Dim: world.End, Synchronous: true}.New()
		at := cube.Pos{0, 64, 0}
		ticks := 8
		if arrow {
			ticks = 20
		}
		runWorld(w, func(tx *world.Tx) {
			tx.SetBlock(at, Target{}, nil)
			typ := world.EntityType(redstoneTNTTestEntityType{})
			if arrow {
				typ = redstoneArrowTestEntityType{}
			}
			projectile := tx.AddEntity(world.EntitySpawnOpts{}.New(typ, redstoneTNTTestEntityConfig{}))
			Target{}.ProjectileHitAt(at, tx, projectile, cube.FaceWest, mgl64.Vec3{0, 64, .5})
			got := tx.Block(at).(Target)
			if got.Power != 1 {
				t.Fatal("edge did not produce minimum power")
			}
			got.ProjectileHitAt(at, tx, projectile, cube.FaceWest, mgl64.Vec3{0, 64.5, .5})
			if tx.Block(at).(Target).Power != 1 {
				t.Error("second hit changed an active pulse")
			}
			if world.NetworkNBT(got) != nil {
				t.Error("server target state was sent as a fake Bedrock block entity")
			}
		})
		for range ticks - 1 {
			w.AdvanceTick()
		}
		runWorld(w, func(tx *world.Tx) {
			if tx.Block(at).(Target).Power != 1 {
				t.Errorf("arrow=%t: target ended early", arrow)
			}
		})
		w.AdvanceTick()
		runWorld(w, func(tx *world.Tx) {
			if tx.Block(at).(Target).Power != 0 {
				t.Errorf("arrow=%t: target exceeded %d ticks", arrow, ticks)
			}
		})
		w.Close()
	}
}

func TestTargetServerStateIsMovableAndSurvivesSave(t *testing.T) {
	target := Target{Power: 11, Expires: 123}
	restored := (Target{}).DecodeNBT(target.EncodeNBT()).(Target)
	if restored.Power != 11 || restored.Expires != 123 {
		t.Fatal("target lost saved server state")
	}
	w := world.Config{Dim: world.End, Synchronous: true}.New()
	defer w.Close()
	runWorld(w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{1, 64, 0}, Target{}, nil)
		r, ok := resolvePiston(tx, cube.Pos{0, 64, 0}, cube.FaceEast, true)
		if !ok || len(r.push) != 1 {
			t.Fatal("server-only target state incorrectly blocked a Java piston")
		}
	})
}
