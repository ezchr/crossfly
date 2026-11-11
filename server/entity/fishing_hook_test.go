package entity

import (
	"context"
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestFishingHookCastVelocity: FishingHook::reloadHardcoded, the look direction at 0.4 plus the owner's velocity,
// normalised, plus gaussian*0.0075 per axis, times 1.5.
func TestFishingHookCastVelocity(t *testing.T) {
	for _, c := range []struct {
		name     string
		rot      cube.Rotation
		ownerVel mgl64.Vec3
		gaussian [3]float64
		want     mgl64.Vec3
	}{
		{"south", cube.Rotation{0, 0}, mgl64.Vec3{}, [3]float64{}, mgl64.Vec3{0, 0, 1.5}},
		{"west", cube.Rotation{90, 0}, mgl64.Vec3{}, [3]float64{}, mgl64.Vec3{-1.5, 0, 0}},
		{"up", cube.Rotation{0, -90}, mgl64.Vec3{}, [3]float64{}, mgl64.Vec3{0, 1.5, 0}},
		{"running sideways", cube.Rotation{0, 0}, mgl64.Vec3{0.4, 0, 0}, [3]float64{}, mgl64.Vec3{1.5 / math.Sqrt2, 0, 1.5 / math.Sqrt2}},
		{"spread", cube.Rotation{0, 0}, mgl64.Vec3{}, [3]float64{1, -2, 0}, mgl64.Vec3{0.0075 * 1.5, -0.015 * 1.5, 1.5}},
	} {
		if got := fishingHookVelocity(c.rot, c.ownerVel, c.gaussian); !hookNear(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	if got := fishingHookVelocity(cube.Rotation{0, 0}, mgl64.Vec3{0, 0, -0.4}, [3]float64{}); got != (mgl64.Vec3{}) {
		t.Errorf("no direction: %v, want zero", got)
	}
}

// hookNear reports whether a and b differ by less than 1e-9 on every axis.
func hookNear(a, b mgl64.Vec3) bool {
	for i := range a {
		if math.Abs(a[i]-b[i]) > 1e-9 {
			return false
		}
	}
	return true
}

func TestFishingHookPullVelocity(t *testing.T) {
	// Level ground, 10 blocks apart: 0.1 of the way to 1 above the owner's head, plus 0.08 of its length up.
	got := fishingHookPullVelocity(mgl64.Vec3{0, 65.62, 0}, mgl64.Vec3{0, 65.62, 10})
	d := mgl64.Vec3{0, 0.1, -1}
	want := mgl64.Vec3{0, 0.1 + d.Len()*0.08, -1}
	if !hookNear(got, want) {
		t.Fatalf("pull %v, want %v", got, want)
	}
}

// hookWorld runs f in a world with the default entities.
func hookWorld(t *testing.T, f func(tx *world.Tx)) {
	t.Helper()
	w := world.Config{Synchronous: true, Entities: DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		f(tx)
		return struct{}{}, nil
	})
}

// TestFishingHookGravity: FishingHook::_updateGravity in air (gravity 0.04, drag 0.92), on the ground (drag 0.5) and
// in water (buoyancy, drag 0.92*0.9).
func TestFishingHookGravity(t *testing.T) {
	hookWorld(t, func(tx *world.Tx) {
		e := tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 100, 0.5}}.New(FishingHookType, fishingHookConfig{})).(*Ent)
		b := e.Behaviour().(*FishingHookBehaviour)

		vel, lava := b.updateGravity(tx, e, e.Position(), mgl64.Vec3{0.1, 0.5, 1})
		if want := (mgl64.Vec3{0.092, (0.5 - 0.04) * 0.92, 0.92}); lava || !hookNear(vel, want) {
			t.Errorf("air: %v, want %v", vel, want)
		}
		b.onGround = true
		vel, _ = b.updateGravity(tx, e, e.Position(), mgl64.Vec3{0.1, 0, 1})
		if want := (mgl64.Vec3{0.05, -0.04 * 0.5, 0.5}); !hookNear(vel, want) {
			t.Errorf("ground: %v, want %v", vel, want)
		}
		b.onGround = false

		// Deep water: the box is all water, the block above too.
		for y := 50; y <= 52; y++ {
			tx.SetBlock(cube.Pos{0, y, 0}, block.Water{Still: true, Depth: 8}, nil)
		}
		pos := mgl64.Vec3{0.5, 50.5, 0.5}
		vel, _ = b.updateGravity(tx, e, pos, mgl64.Vec3{0, -0.2, 0})
		vy := math.Min(1.01*0.15, (-0.2+0.04)*0.8+0.05) * 0.92 * 0.9
		if !hookNear(vel, mgl64.Vec3{0, vy, 0}) {
			t.Errorf("deep water: %v, want y %v", vel, vy)
		}

		// A hook floating at the surface of water settles there and bobs. It needs an owner to stay out.
		b.owner = tx.AddEntity(NewSnowball(world.EntitySpawnOpts{Position: mgl64.Vec3{3, 60, 3}}, nil)).H()
		e.data.Pos, e.data.Vel = mgl64.Vec3{0.5, 53, 0.5}, mgl64.Vec3{}
		lo, hi := math.Inf(1), math.Inf(-1)
		for i := 0; i < 200; i++ {
			b.Tick(e, tx)
			if i > 100 {
				lo, hi = math.Min(lo, e.Position()[1]), math.Max(hi, e.Position()[1])
			}
		}
		if lo < 52.5 || hi > 53.2 || hi-lo < 0.001 {
			t.Errorf("floating hook between %v and %v, want bobbing at the surface (53)", lo, hi)
		}

		// Lava retrieves (removes) the hook.
		tx.SetBlock(cube.Pos{5, 50, 5}, block.Lava{Still: true, Depth: 8}, nil)
		if _, lava := b.updateGravity(tx, e, mgl64.Vec3{5.5, 50.2, 5.5}, mgl64.Vec3{}); !lava {
			t.Error("lava not noticed")
		}
	})
}

// TestFishingHookNoOwner: a hook without an owner (one loaded from disk) removes itself.
func TestFishingHookNoOwner(t *testing.T) {
	hookWorld(t, func(tx *world.Tx) {
		data := &world.EntityData{}
		FishingHookType.DecodeNBT(nil, data)
		if b, ok := data.Data.(*FishingHookBehaviour); !ok || b.Owner() != nil {
			t.Fatalf("loaded hook %#v", data.Data)
		}
		h := world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 100, 0.5}}.New(FishingHookType, fishingHookConfig{})
		e := tx.AddEntity(h).(*Ent)
		e.Tick(tx, 0)
		if _, ok := h.Entity(tx); ok {
			t.Fatal("ownerless hook kept")
		}
	})
}

func TestHurtsWithoutDamage(t *testing.T) {
	hookWorld(t, func(tx *world.Tx) {
		hook := tx.AddEntity(world.EntitySpawnOpts{}.New(FishingHookType, fishingHookConfig{}))
		snowball := tx.AddEntity(NewSnowball(world.EntitySpawnOpts{}, nil))
		if !HurtsWithoutDamage(ProjectileDamageSource{Projectile: hook}) {
			t.Error("hook hit does not hurt")
		}
		if HurtsWithoutDamage(ProjectileDamageSource{Projectile: snowball}) || HurtsWithoutDamage(VoidDamageSource{}) {
			t.Error("other sources hurt without damage")
		}
	})
}
