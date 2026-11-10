//go:build crossfly_full

package javaspawn

import (
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
)

func TestMain(m *testing.M) {
	// Rails, levers and the other redstone blocks only exist with Java
	// redstone, which Crossfly Full servers turn on.
	block.RegisterJavaRedstone()
	world.DefaultBlockRegistry.Finalize()
	os.Exit(m.Run())
}
