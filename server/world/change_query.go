package world

import "github.com/df-mc/dragonfly/server/block/cube"

// BlockChangeSeq returns the world's block change counter: it grows with
// every block written. Pass it to SectionChangedSince later to find out if a
// chunk section changed in between. Crossfly's Java mob mode keeps its
// points of interest (beds, workstations, bells) in sync with the blocks
// this way, like Java's PoiManager follows ServerLevel.onBlockStateChange.
func (tx *Tx) BlockChangeSeq() uint64 {
	return tx.World().changes.seq
}

// SectionChangedSince reports whether a block of the chunk section holding
// pos was written after seq (a BlockChangeSeq value). Changes that only move
// redstone power are not logged. After very many sections changed the log
// forgets old sections and this may report false for an old change: callers
// rescan now and then anyway.
func (tx *Tx) SectionChangedSince(pos cube.Pos, seq uint64) bool {
	l := &tx.World().changes
	key, ok := sectionKey(pos[0], pos[1], pos[2])
	if !ok {
		return l.seq > seq
	}
	return l.sections[key] > seq
}
