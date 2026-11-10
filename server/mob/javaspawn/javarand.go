//go:build crossfly_full

package javaspawn

import "math"

// javaRandom is java.util.Random (LegacyRandomSource), for the slime chunks.
type javaRandom struct{ seed int64 }

func newJavaRandom(seed int64) *javaRandom {
	return &javaRandom{seed: (seed ^ 0x5DEECE66D) & (1<<48 - 1)}
}

func (r *javaRandom) next(bits uint) int32 {
	r.seed = (r.seed*0x5DEECE66D + 0xB) & (1<<48 - 1)
	return int32(r.seed >> (48 - bits))
}

// nextInt is Random.nextInt(bound).
func (r *javaRandom) nextInt(bound int32) int32 {
	if bound&(-bound) == bound {
		return int32((int64(bound) * int64(r.next(31))) >> 31)
	}
	for {
		bits := r.next(31)
		val := bits % bound
		if bits-val+(bound-1) >= 0 {
			return val
		}
	}
}

func floor(v float64) int { return int(math.Floor(v)) }
func ceil(v float64) int  { return int(math.Ceil(v)) }
