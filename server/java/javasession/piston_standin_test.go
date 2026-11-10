//go:build crossfly_full

package javasession

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
)

// TestMovingChestShownWhereItLands: with redstone = "dragonfly" a Java client is shown a chest a piston moves
// where it lands, as a moving block it could not draw; a barrel (drawn from its block state) stays a moving block.
func TestMovingChestShownWhereItLands(t *testing.T) {
	world.SetJavaRedstone(false)
	defer world.SetJavaRedstone(true)
	if b, ok := javaShown(block.MovingPiston{Moving: block.NewChest()}); !ok {
		t.Errorf("moving chest not shown as a chest: %T", b)
	} else if _, chest := b.(block.Chest); !chest {
		t.Errorf("moving chest shown as %T", b)
	}
	if _, ok := javaShown(block.MovingPiston{Moving: block.NewBarrel()}); ok {
		t.Error("moving barrel replaced")
	}
	if _, ok := javaShown(block.Piston{Attached: nil}); ok {
		t.Error("piston replaced")
	}
	world.SetJavaRedstone(true)
	if _, ok := javaShown(block.MovingPiston{Moving: block.NewChest()}); ok {
		t.Error("stand-in used with Java redstone")
	}
}
