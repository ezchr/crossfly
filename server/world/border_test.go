package world

import (
	"testing"
	"time"

	"github.com/go-gl/mathgl/mgl64"
)

func TestBorderShape(t *testing.T) {
	b := Border{BorderConfig: BorderConfig{Center: mgl64.Vec2{10, -10}, Size: 20}}
	if !b.Contains(mgl64.Vec3{0, 64, -20}) || !b.Contains(mgl64.Vec3{20, 0, 0}) {
		t.Fatal("corners should be inside")
	}
	if b.Contains(mgl64.Vec3{21, 64, 0}) {
		t.Fatal("past the east edge should be outside")
	}
	if d := b.DistanceOutside(mgl64.Vec3{23, 64, 0}); d != 3 {
		t.Fatalf("3 blocks out measured %v", d)
	}
	if d := b.DistanceOutside(mgl64.Vec3{23, 64, 4}); d != 5 {
		t.Fatalf("3 and 4 out measured %v", d)
	}
	if c := b.Clamp(mgl64.Vec3{25, 64, -30}, 0.3); c != (mgl64.Vec3{19.7, 64, -19.7}) {
		t.Fatalf("clamped to %v", c)
	}
	if (Border{BorderConfig: BorderConfig{Size: BorderMaxSize}}).Enabled() {
		t.Fatal("the largest size is no border")
	}
}

func TestBorderMove(t *testing.T) {
	w := &World{}
	if w.Border().Enabled() {
		t.Fatal("a new world has no border")
	}
	w.SetBorder(DefaultBorderConfig(mgl64.Vec2{}, 100))
	v := w.Border().Version
	w.MoveBorder(50, time.Second)
	b := w.Border()
	if !b.Shrinking() || b.TargetSize != 50 || b.Size > 100 || b.Size < 50 || b.Version == v {
		t.Fatalf("moving border %+v", b)
	}
	time.Sleep(1100 * time.Millisecond)
	if b := w.Border(); b.Size != 50 || b.Remaining != 0 || b.Shrinking() {
		t.Fatalf("after the move %+v", b)
	}
	w.SetBorder(BorderConfig{})
	if w.Border().Enabled() {
		t.Fatal("size 0 removes the border")
	}
}
