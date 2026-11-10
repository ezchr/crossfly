package block

import "github.com/df-mc/dragonfly/server/block/cube"

// BellRingAction is a world.BlockAction that swings a bell (Crossfly Full). Face is the side the bell swings
// towards, Direction the same side as Bedrock's legacy direction (0 south, 1 west, 2 north, 3 east).
type BellRingAction struct {
	action
	Face      cube.Face
	Direction int32
}
