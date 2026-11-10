//go:build crossfly_full

package jnether

import (
	"math"
	"math/rand/v2"
	"testing"
)

// TestShulkerDuplicationOdds: 1 - (n-1)/5 for n shulkers around.
func TestShulkerDuplicationOdds(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	const tries = 200000
	for n := 1; n <= 7; n++ {
		hits := 0
		for range tries {
			if ShulkerDuplicates(n, r.Float32()) {
				hits++
			}
		}
		want := math.Max(0, 1-float64(n-1)/5)
		if got := float64(hits) / tries; math.Abs(got-want) > 0.01 {
			t.Errorf("%d shulkers: duplicated %.3f of the time, want %.3f", n, got, want)
		}
	}
}
