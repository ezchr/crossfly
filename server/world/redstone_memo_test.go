package world

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

func TestPosSet(t *testing.T) {
	var s posSet
	positions := []cube.Pos{{0, 0, 0}, {-1, -64, -1}, {15, 15, 15}, {16, 16, 16}, {-17, 300, 33}, {1 << 30, 5, -(1 << 30)}}
	for _, pos := range positions {
		if s.has(pos) {
			t.Fatalf("empty set has %v", pos)
		}
		if !s.add(pos) {
			t.Fatalf("add %v reported it present", pos)
		}
		if s.add(pos) {
			t.Fatalf("second add %v reported it new", pos)
		}
	}
	for _, pos := range positions {
		if !s.has(pos) {
			t.Fatalf("set lost %v", pos)
		}
	}
	for _, pos := range []cube.Pos{{1, 0, 0}, {-1, -63, -1}, {15, 15, 14}, {-16, 300, 33}} {
		if s.has(pos) {
			t.Fatalf("set has %v, never added", pos)
		}
	}
	if s.len() != len(positions) {
		t.Fatalf("len %v, want %v", s.len(), len(positions))
	}
	s.clear()
	for _, pos := range positions {
		if s.has(pos) {
			t.Fatalf("cleared set has %v", pos)
		}
	}
}

func TestChangeLog(t *testing.T) {
	w := &World{}
	pos := cube.Pos{10, 64, 10}
	seq := w.changes.seq
	w.blockChangedAt(cube.Pos{14, 64, 10})
	if !w.changes.unchangedSince(seq, pos, 3) {
		t.Fatal("a change 4 blocks away counted within 3")
	}
	if w.changes.unchangedSince(seq, pos, 4) {
		t.Fatal("a change 4 blocks away not counted within 4")
	}
	seq = w.changes.seq
	for i := 0; i < changeLogScan; i++ {
		w.blockChangedAt(cube.Pos{100, 64, 100})
	}
	if !w.changes.unchangedSince(seq, pos, 3) {
		t.Fatal("far changes up to the scan limit counted")
	}
	w.blockChangedAt(cube.Pos{100, 64, 100})
	if w.changes.unchangedSince(seq, pos, 3) {
		t.Fatal("more changes than the scan limit kept a value")
	}
	seq = w.changes.seq
	w.everyBlockChanged()
	if w.changes.unchangedSince(seq, pos, 3) {
		t.Fatal("a change of every block kept a value")
	}
	seq = w.changes.seq
	if !w.changes.unchangedSince(seq, pos, 3) {
		t.Fatal("a value worked out after a change of every block was dropped")
	}
}

func TestRedstoneMemoAfterSetBlock(t *testing.T) {
	w := Config{Synchronous: true, Blocks: redstoneSignalLossTestRegistry()}.New()
	defer w.Close()
	pos := cube.Pos{0, 64, 0}
	runWorld(w, func(tx *Tx) {
		tx.SetBlock(pos, redstoneLossRelayer{}, nil)
		_, _, seq := tx.RedstoneMemo(RedstoneMemoWireLinks, pos)
		tx.SetRedstoneMemo(RedstoneMemoWireLinks, pos, seq, 7)
		if v, ok, _ := tx.RedstoneMemo(RedstoneMemoWireLinks, pos); !ok || v != 7 {
			t.Fatalf("memo %v %v, want 7", v, ok)
		}
		tx.SetBlock(cube.Pos{10, 64, 10}, redstoneLossRelayer{}, nil) // In the same chunk: loading one changes every block.
		if v, ok, _ := tx.RedstoneMemo(RedstoneMemoWireLinks, pos); !ok || v != 7 {
			t.Fatal("memo dropped by a change far away")
		}
		tx.SetBlock(cube.Pos{2, 65, 0}, redstoneLossRelayer{}, nil)
		if _, ok, _ := tx.RedstoneMemo(RedstoneMemoWireLinks, pos); ok {
			t.Fatal("memo kept after a change two blocks away")
		}
	})
}
