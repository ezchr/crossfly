//go:build crossfly_full

package block

import "github.com/df-mc/dragonfly/server/world"

// JavaStandIn returns the block a moving block carries, with redstone = "dragonfly": a Java client cannot draw a
// moving block that only its block entity draws (a chest), and is shown it where it arrives instead
// (javasession.javaShown). With Java redstone such blocks never move.
func (m MovingPiston) JavaStandIn() world.Block {
	if world.JavaRedstone() || m.Source {
		return nil
	}
	return m.Moving
}
