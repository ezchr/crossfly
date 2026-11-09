//go:build crossfly_full

package world

// RedstoneDirtyLenForTest returns the number of positions queued for the next redstone engine evaluation.
func RedstoneDirtyLenForTest(w *World) int {
	if w.redstone == nil {
		return 0
	}
	return len(w.redstone.dirty)
}

// RedstoneTickForTest runs one redstone engine evaluation in tx.
func RedstoneTickForTest(tx *Tx) {
	tx.World().redstone.tick(tx, tx.CurrentTick())
}
