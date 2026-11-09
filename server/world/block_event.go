//go:build crossfly_full

package world

import "github.com/df-mc/dragonfly/server/block/cube"

// BlockEventHandler processes server block events, after scheduled ticks and before block entity ticks.
type BlockEventHandler interface {
	BlockEvent(pos cube.Pos, tx *Tx, event int)
}

// QueueBlockEvent queues one event per position, block type and event value. Rechecks belong in the handler:
// the circuit may change again between scheduling an event and executing it.
func (tx *Tx) QueueBlockEvent(pos cube.Pos, b Block, event int) {
	hash, _ := b.Hash()
	for _, e := range tx.w.blockEvents {
		h, _ := e.block.Hash()
		if e.pos == pos && h == hash && e.event == event {
			return
		}
	}
	tx.w.blockEvents = append(tx.w.blockEvents, blockEvent{pos, b, event})
}
func (w *World) runBlockEvents(tx *Tx, after func()) {
	// Bound pathological feedback just as neighbour/redstone propagation is bounded. Remaining events wait a tick.
	for count := 0; len(w.blockEvents) > 0 && count < 65536; count++ {
		e := w.blockEvents[0]
		w.blockEvents = w.blockEvents[1:]
		current := tx.Block(e.pos)
		want, _ := e.block.Hash()
		got, _ := current.Hash()
		if want == got {
			if handler, ok := current.(BlockEventHandler); ok {
				handler.BlockEvent(e.pos, tx, e.event)
				// Java runs the neighbour updates of an event (a piston moving blocks) right away, before the
				// next event: a piston they turn off queues its event in this same run, before block entities
				// (moving blocks) tick.
				if after != nil {
					after()
				}
			}
		}
	}
	if len(w.blockEvents) == 0 {
		w.blockEvents = nil
	}
}
