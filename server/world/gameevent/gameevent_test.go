package gameevent

import (
	"testing"

	"github.com/go-gl/mathgl/mgl64"
)

// TestMovementSteps: walking posts a step each further distance unit (three per block, as BDS weighs
// horizontal movement by 3), swimming posts swims, and nothing comes from standing still.
func TestMovementSteps(t *testing.T) {
	var m Movement
	steps := 0
	for i := 0; i <= 8; i++ {
		ev, ok := m.Track(MoveState{Pos: mgl64.Vec3{0, 1, float64(i) * 0.25}, OnGround: true, EmitMove: true, EmitSwim: true})
		if ok {
			if ev != EntityMove {
				t.Fatalf("walking posted %v", ev.Name)
			}
			steps++
		}
	}
	// 0.75 units a move: a step at 0.75, 1.5, 2.25, 3, 4.5, 5.25 and 6.
	if steps != 7 {
		t.Errorf("%d steps over 2 blocks, want 7", steps)
	}
	for i := 0; i < 10; i++ {
		if ev, ok := m.Track(MoveState{Pos: mgl64.Vec3{0, 1, 2}, OnGround: true, EmitMove: true}); ok {
			t.Fatalf("standing still posted %v", ev.Name)
		}
	}
	var s Movement
	swims := 0
	for i := 0; i <= 10; i++ {
		if ev, ok := s.Track(MoveState{Pos: mgl64.Vec3{float64(i) * 0.2, 1, 0}, InLiquid: true, EmitMove: true, EmitSwim: true}); ok && ev == Swim {
			swims++
		}
	}
	if swims == 0 {
		t.Error("swimming posted no swim")
	}
}

// TestMovementLandingAndFlaps: landing after a jump or fall hits the ground once, and flying posts a
// flap every 10 ticks only for those that flap.
func TestMovementLandingAndFlaps(t *testing.T) {
	var m Movement
	ys := []float64{1, 1.4, 1.8, 1.9, 1.5, 1.0}
	var got []string
	for i, y := range ys {
		ev, ok := m.Track(MoveState{Pos: mgl64.Vec3{0, y, 0}, OnGround: i == 0 || i == len(ys)-1, EmitMove: true})
		if ok {
			got = append(got, ev.Name)
		}
	}
	if len(got) != 1 || got[0] != HitGround.Name {
		t.Errorf("a jump posted %v, want one hit_ground", got)
	}
	for _, flap := range []bool{false, true} {
		var f Movement
		n := 0
		for tick := int64(0); tick < 40; tick++ {
			if ev, ok := f.Track(MoveState{Pos: mgl64.Vec3{0, 10, float64(tick)}, Tick: tick, EmitFlap: flap}); ok && ev == Flap {
				n++
			}
		}
		if want := map[bool]int{false: 0, true: 4}[flap]; n != want {
			t.Errorf("emit_flap %v: %d flaps in 40 ticks, want %d", flap, n, want)
		}
	}
}

// TestPostWithoutListeners: posting does nothing (and calls no listener) in a world without vibration
// listeners.
func TestPostWithoutListeners(t *testing.T) {
	if Active(nil) {
		t.Fatal("Active(nil)")
	}
	PostContext(nil, BlockPlace, mgl64.Vec3{}, Context{})
}
