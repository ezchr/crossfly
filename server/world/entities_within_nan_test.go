package world_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestEntitiesWithinHugeBox(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer func() { _ = w.Close() }()
	m := math.MaxFloat64
	for _, box := range []cube.BBox{
		cube.Box(-m, -m, -m, m, m, m),
		cube.Box(-3e7, 0, -3e7, 3e7, 256, 3e7),
		cube.Box(math.NaN(), 0, 0, 1, 1, 1),
		cube.Box(math.Inf(-1), 0, math.Inf(-1), math.Inf(1), 1, math.Inf(1)),
	} {
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
				for range tx.EntitiesWithin(box) {
				}
				return struct{}{}, nil
			})
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("EntitiesWithin(%v) hung", box)
		}
	}
}
