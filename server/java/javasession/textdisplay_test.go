package javasession

import (
	"math"
	"testing"
)

func TestTextDisplayShift(t *testing.T) {
	for _, c := range []struct {
		tag  string
		want float64
	}{
		{"", 0},
		{"one line", 0.35 - 0.1414},
		{"a\nb", 0.35 - 2*0.1414},
		{"title\n1\n2\n3\n4\n5\n6\n7\n8\n9\n10", 0.35 - 11*0.1414}, // a leaderboard: 1.205 down
	} {
		if got := float64(textDisplayShift(c.tag)); math.Abs(got-c.want) > 1e-4 {
			t.Errorf("textDisplayShift(%q) = %v, want %v", c.tag, got, c.want)
		}
	}
}
