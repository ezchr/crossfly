//go:build crossfly_full

package mob

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// TurtleHatched gives a turtle that hatched from turtle eggs the eggs' block as its home (TurtleEggBlock::tick
// sets the HomeComponent of every turtle it hatches), where it comes back to lay its own eggs.
func (BlockHost) TurtleHatched(_ *world.Tx, turtle world.Entity, home cube.Pos) {
	if m, ok := Of(turtle); ok {
		m.SetHome(home)
	}
}
