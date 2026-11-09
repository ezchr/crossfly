//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func NewTrappedChest() Chest { c := NewChest(); c.Trapped = true; return c }

// ContainerRedstoneViewer distinguishes spectator viewers from players that count as openers.
// The transaction belongs to the container world and must not be re-entered by implementations.
type ContainerRedstoneViewer interface{ CountsForContainerPower(*world.Tx) bool }

func (c Chest) RedstonePower(cube.Pos, *world.Tx, cube.Face) int {
	if c.Trapped {
		return c.signal
	}
	return 0
}
func (c Chest) RedstoneStrongPower(_ cube.Pos, _ *world.Tx, face cube.Face) int {
	if c.Trapped && face == cube.FaceDown {
		return c.signal
	}
	return 0
}
func (c Chest) Tick(tick int64, pos cube.Pos, tx *world.Tx) {
	if c.Trapped && tick%5 == 0 {
		c.updateTrappedPower(pos, tx)
	}
}
func (c Chest) updateTrappedPower(pos cube.Pos, tx *world.Tx) {
	if !c.Trapped || c.viewerMu == nil {
		return
	}
	c.viewerMu.RLock()
	count := 0
	for viewer := range c.viewers {
		if v, ok := viewer.(ContainerRedstoneViewer); ok && !v.CountsForContainerPower(tx) {
			continue
		}
		count++
	}
	c.viewerMu.RUnlock()
	power := min(15, count)
	if power == c.signal {
		return
	}
	c.signal = power
	tx.SetBlock(pos, c, nil)
	c.notifyTrappedBelow(pos, tx)
	if c.paired {
		other := c.pairPos(pos)
		if pair, ok := tx.Block(other).(Chest); ok && pair.Trapped && pair.signal != power {
			pair.signal = power
			tx.SetBlock(other, pair, nil)
			pair.notifyTrappedBelow(other, tx)
		}
	}
}
func (Chest) notifyTrappedBelow(pos cube.Pos, tx *world.Tx) {
	below := pos.Side(cube.FaceDown)
	for _, face := range cube.Faces() {
		tx.NotifyNeighbour(below.Side(face), below)
	}
}

// JavaRedstoneExtension: chests are redstone components (trapped chests give power) only with Java redstone.
func (Chest) JavaRedstoneExtension() {}
