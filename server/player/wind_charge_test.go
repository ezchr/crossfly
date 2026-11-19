package player

import "testing"

func TestWindLaunchLimitFall(t *testing.T) {
	var w windLaunch
	if got := w.limitFall(20, 60); got != 20 {
		t.Fatalf("no launch: fall %v, want 20", got)
	}
	w = windLaunch{active: true, y: 64}
	for _, c := range []struct{ fall, y, want float64 }{
		{20, 64, 0}, // landed where it was launched
		{20, 70, 0}, // landed above it
		{20, 60, 4}, // 4 blocks below it
		{2, 60, 2},  // fell less than that
	} {
		if got := w.limitFall(c.fall, c.y); got != c.want {
			t.Errorf("fall %v landing at %v: %v, want %v", c.fall, c.y, got, c.want)
		}
	}
}
