//go:build crossfly_full

package village

import "math/rand/v2"

func testRand() *rand.Rand { return rand.New(rand.NewPCG(1, 2)) }
