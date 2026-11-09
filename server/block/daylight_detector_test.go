//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"testing"
)

func TestDaylightDetectorBasicSkyAndWeather(t *testing.T) {
	for _, c := range []struct {
		sky, time     int
		inverted      bool
		rain, thunder float32
		want          int
	}{
		{15, 6000, false, 0, 0, 15},
		{15, 18000, false, 0, 0, 0},
		{15, 6000, true, 0, 0, 0},
		{15, 18000, true, 0, 0, 11},
		{0, 6000, false, 0, 0, 0},
		{0, 6000, true, 0, 0, 15},
		{15, 6000, false, 1, 0, 12},
		{15, 6000, false, 1, 1, 10},
	} {
		if got := daylightPower(c.sky, c.time, c.inverted, c.rain, c.thunder); got != c.want {
			t.Errorf("sky=%d time=%d inverted=%t rain=%v thunder=%v: %d want %d", c.sky, c.time, c.inverted, c.rain, c.thunder, got, c.want)
		}
	}
	model := (DaylightDetector{}).Model()
	if model.FaceSolid(cube.Pos{}, cube.FaceUp, nil) {
		t.Error("sensor incorrectly has a full top support face")
	}
	if got := model.BBox(cube.Pos{}, nil)[0].Height(); got != .375 {
		t.Errorf("wrong sensor height %v", got)
	}
}
