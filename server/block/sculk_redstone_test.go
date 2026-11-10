//go:build crossfly_full

package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// The sculk sensors register with the mob blocks.
func init() { RegisterMobBlocks() }

// TestSculkSensorComparatorReadings: a comparator reads the frequency of the last vibration from an active
// sensor, plain or calibrated, and 0 from an inactive or cooling one; a beehive its honey level and a
// copper golem statue its pose.
func TestSculkSensorComparatorReadings(t *testing.T) {
	for phase, want := range []int{0, 7, 0} {
		for _, b := range []world.Block{
			SculkSensor{Phase: phase, Power: 5, Frequency: 7},
			CalibratedSculkSensor{Facing: cube.East, Phase: phase, Power: 5, Frequency: 7},
		} {
			if got, ok := redstoneAnalogOutput(cube.Pos{}, b, nil); !ok || got != want {
				t.Errorf("%#v: comparator reads %d (%v), want %d", b, got, ok, want)
			}
		}
	}
	for level := 0; level <= 5; level++ {
		if got, ok := redstoneAnalogOutput(cube.Pos{}, Beehive{HoneyLevel: level}, nil); !ok || got != level {
			t.Errorf("beehive honey %d: comparator reads %d", level, got)
		}
	}
	for i, p := range []CopperGolemPose{StandingPose(), SittingPose(), RunningPose(), StarPose()} {
		if got, ok := redstoneAnalogOutput(cube.Pos{}, CopperGolemStatue{Pose: p}, nil); !ok || got != i+1 {
			t.Errorf("statue pose %v: comparator reads %d, want %d", p.Name(), got, i+1)
		}
	}
}

// TestCalibratedSculkSensorSignal: an active calibrated sensor powers every side but its back, and
// strongly the block below it, as the plain one does.
func TestCalibratedSculkSensorSignal(t *testing.T) {
	s := CalibratedSculkSensor{Facing: cube.North, Phase: 1, Power: 9}
	for _, f := range cube.Faces() {
		want := 9
		if f == cube.FaceSouth {
			want = 0
		}
		if got := s.RedstonePower(cube.Pos{}, nil, f); got != want {
			t.Errorf("facing north, power out of %v: %d, want %d", f, got, want)
		}
		strong := 0
		if f == cube.FaceDown {
			strong = 9
		}
		if got := s.RedstoneStrongPower(cube.Pos{}, nil, f); got != strong {
			t.Errorf("facing north, strong power out of %v: %d, want %d", f, got, strong)
		}
		if got := (SculkSensor{Phase: 1, Power: 9}).RedstoneStrongPower(cube.Pos{}, nil, f); got != strong {
			t.Errorf("sculk sensor strong power out of %v: %d, want %d", f, got, strong)
		}
	}
	s.Phase = 2
	if got := s.RedstonePower(cube.Pos{}, nil, cube.FaceUp); got != 0 {
		t.Errorf("a cooling sensor gives %d", got)
	}
}

// TestCalibratedSculkSensorInput: the input is the signal into the back of the sensor only.
func TestCalibratedSculkSensorInput(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	runWorld(w, func(tx *world.Tx) {
		pos := cube.Pos{0, 64, 0}
		s := CalibratedSculkSensor{Facing: cube.North}
		tx.SetBlock(pos, s, nil)
		if got := s.InputStrength(pos, tx); got != 0 {
			t.Errorf("input %d with nothing behind", got)
		}
		tx.SetBlock(pos.Side(cube.FaceEast), RedstoneBlock{}, nil)
		if got := s.InputStrength(pos, tx); got != 0 {
			t.Errorf("input %d from a redstone block on the side", got)
		}
		tx.SetBlock(pos.Side(cube.FaceSouth), RedstoneBlock{}, nil)
		if got := s.InputStrength(pos, tx); got != 15 {
			t.Errorf("input %d from a redstone block behind, want 15", got)
		}
	})
}

// TestJukeboxRedstoneSignal: a jukebox gives a signal of 15 while its disc plays, none once the song is
// over or without a disc.
func TestJukeboxRedstoneSignal(t *testing.T) {
	disc := item.NewStack(item.MusicDisc{DiscType: sound.Disc11()}, 1)
	for _, c := range []struct {
		j    Jukebox
		want int
	}{
		{Jukebox{}, 0},
		{Jukebox{Item: disc, handle: 3}, 15},
		{Jukebox{Item: disc}, 0},
	} {
		if got := c.j.RedstonePower(cube.Pos{}, nil, cube.FaceNorth); got != c.want {
			t.Errorf("%+v: signal %d, want %d", c.j, got, c.want)
		}
	}
}
