//go:build crossfly_full

package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/go-gl/mathgl/mgl64"
)

// The pure rules of redstone=dragonfly's diodes and sensors (dfrs_diode_*.go); the behaviour in a world is tested in
// server/block/dfrsdiodetest.

func TestBedrockDaylightPower(t *testing.T) {
	for _, test := range []struct {
		name     string
		time     int
		inverted bool
		rain     float32
		want     int
	}{
		{name: "noon", time: 6000, want: 15},
		{name: "midnight", time: 18000, want: 0},
		{name: "midnight inverted", time: 18000, inverted: true, want: 11},
		{name: "noon inverted", time: 6000, inverted: true, want: 0},
		{name: "noon rain", time: 6000, rain: 1, want: 12},
	} {
		if got := bedrockDaylightPower(15, test.time, true, test.inverted, test.rain, 0); got != test.want {
			t.Errorf("%s: %d, want %d", test.name, got, test.want)
		}
	}
	// Under a roof the sky light is lower and so is the signal.
	if got := bedrockDaylightPower(10, 6000, true, false, 0, 0); got != 10 {
		t.Errorf("sky light 10 at noon: %d, want 10", got)
	}
}

func TestBedrockTargetStrength(t *testing.T) {
	for _, test := range []struct {
		name string
		face cube.Face
		hit  mgl64.Vec3
		want int
	}{
		{name: "centre", face: cube.FaceUp, hit: mgl64.Vec3{0.5, 1, 0.5}, want: 15},
		{name: "edge middle", face: cube.FaceUp, hit: mgl64.Vec3{1, 1, 0.5}, want: 4},
		{name: "corner", face: cube.FaceNorth, hit: mgl64.Vec3{0, 0, 0}, want: 1},
		{name: "quarter", face: cube.FaceEast, hit: mgl64.Vec3{1, 0.25, 0.5}, want: 10},
	} {
		if got := bedrockTargetStrength(test.face, test.hit); got != test.want {
			t.Errorf("%s: %d, want %d", test.name, got, test.want)
		}
	}
}

func TestBedrockContainerSignal(t *testing.T) {
	hopper := inventory.New(5, nil)
	_ = hopper.SetItem(0, item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1))
	if got := bedrockContainerSignal(hopper); got != 3 {
		t.Errorf("one sword in a hopper: %d, want 3", got)
	}
	chest := inventory.New(27, nil)
	if got := bedrockContainerSignal(chest); got != 0 {
		t.Errorf("empty chest: %d, want 0", got)
	}
	_ = chest.SetItem(3, item.NewStack(item.Snowball{}, 1))
	if got := bedrockContainerSignal(chest); got != 1 {
		t.Errorf("one snowball in a chest: %d, want 1", got)
	}
	for i := range 27 {
		_ = chest.SetItem(i, item.NewStack(item.Snowball{}, 16))
	}
	if got := bedrockContainerSignal(chest); got != 15 {
		t.Errorf("full chest of snowballs: %d, want 15", got)
	}
}

// lineTrace runs a repeater's line over inputs (one per game tick) and returns its output per tick.
func lineTrace(r RedstoneRepeater, inputs []bool) []bool {
	var l dfRepeaterLine
	l.setFlag(dfLineValid, true)
	l.setField(dfLineDelay, 2, r.Delay)
	l.fill(r.dfLength(), dfEntryOffSettled)
	out := make([]bool, len(inputs))
	for i, in := range inputs {
		t := int64(i + 1)
		r.dfAdvance(&l, t)
		out[i] = l.dfOutput()
		l.dfRecordInput(in, false, t)
	}
	return out
}

func TestRepeaterLineRules(t *testing.T) {
	pulse := func(start, n, total int) []bool {
		v := make([]bool, total)
		for i := start; i < start+n && i < total; i++ {
			v[i] = true
		}
		return v
	}
	count := func(v []bool) (n int) {
		for _, b := range v {
			if b {
				n++
			}
		}
		return n
	}
	// A one-tick pulse comes out as long as the delay, the delay later.
	for delay := range 4 {
		r := RedstoneRepeater{Delay: delay}
		out := lineTrace(r, pulse(0, 1, 30))
		first := -1
		for i, v := range out {
			if v {
				first = i
				break
			}
		}
		if first != r.dfLength() || count(out) != r.dfLength() {
			t.Errorf("delay %d: one-tick pulse out at %d for %d ticks, want at %d for %d", delay+1, first, count(out), r.dfLength(), r.dfLength())
		}
	}
	// A long pulse keeps its length.
	if out := lineTrace(RedstoneRepeater{Delay: 1}, pulse(0, 9, 30)); count(out) != 9 {
		t.Errorf("9-tick pulse through delay 2: %d ticks on, want 9", count(out))
	}
}
