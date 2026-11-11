package block_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type nanExplosion struct {
	pos  mgl64.Vec3
	size float64
}

func (e nanExplosion) Position() mgl64.Vec3 { return e.pos }
func (e nanExplosion) Size() float64        { return e.size }

func TestExplosionNonFinite(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer func() { _ = w.Close() }()
	for _, src := range []nanExplosion{
		{mgl64.Vec3{0, 64, 0}, math.Inf(1)},
		{mgl64.Vec3{0, 64, 0}, math.NaN()},
		{mgl64.Vec3{math.NaN(), 64, 0}, 4},
		{mgl64.Vec3{0, math.Inf(-1), 0}, 4},
	} {
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
				block.ExplosionConfig{}.Explode(tx, src)
				return struct{}{}, nil
			})
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("explosion %v hung", src)
		}
	}
}
