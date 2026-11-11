package player

import (
	"fmt"
	"log/slog"
	"math"
	"sync"

	"github.com/go-gl/mathgl/mgl64"
)

// finite reports whether f is neither NaN nor infinite.
func finite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// finiteVec3 reports whether every component of v is finite.
func finiteVec3(v mgl64.Vec3) bool {
	return finite(v[0]) && finite(v[1]) && finite(v[2])
}

var nonFiniteOnce sync.Once

// logNonFinite logs, once per process, that a non-finite value was refused.
func logNonFinite(what string, v mgl64.Vec3) {
	nonFiniteOnce.Do(func() {
		slog.Default().Warn("player: refused non-finite value", "kind", what, "value", fmt.Sprint(v))
	})
}
