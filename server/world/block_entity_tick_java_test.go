//go:build crossfly_full

package world

import (
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// beTickLog records the IDs of the beTickTicker blocks in the order they ticked.
var beTickLog []int

// beTickTicker is a ticking block entity. When it ticks it replaces the blocks at Converts with tickers.
type beTickTicker struct {
	ID       int
	Converts []beTickConvert
}

type beTickConvert struct {
	pos cube.Pos
	id  int
}

func (b beTickTicker) Tick(_ int64, pos cube.Pos, tx *Tx) {
	beTickLog = append(beTickLog, b.ID)
	for _, c := range b.Converts {
		tx.SetBlock(c.pos, beTickTicker{ID: c.id}, nil)
	}
	if len(b.Converts) > 0 {
		tx.SetBlock(pos, beTickTicker{ID: b.ID}, nil)
	}
}
func (b beTickTicker) EncodeNBT() map[string]any           { return map[string]any{} }
func (b beTickTicker) DecodeNBT(map[string]any) any        { return b }
func (beTickTicker) EncodeBlock() (string, map[string]any) { return "test:be_ticker", nil }
func (beTickTicker) Hash() (uint64, uint64)                { return 1 << 47, 0 }
func (beTickTicker) Model() BlockModel                     { return redstoneCancellationModel{} }

// beTickPlain is a block entity that does not tick.
type beTickPlain struct{}

func (beTickPlain) EncodeNBT() map[string]any             { return map[string]any{} }
func (b beTickPlain) DecodeNBT(map[string]any) any        { return b }
func (beTickPlain) EncodeBlock() (string, map[string]any) { return "test:be_plain", nil }
func (beTickPlain) Hash() (uint64, uint64)                { return 1 << 48, 0 }
func (beTickPlain) Model() BlockModel                     { return redstoneCancellationModel{} }

// TestBlockEntityTickOrderWithChangedEntities checks that a block entity that becomes a ticking one during the block
// entity tick ticks in the same tick exactly when the full creation-ordered list would have reached it: one set in
// place of a block entity created before the ticking one does not tick until the next tick, one created after it does.
func TestBlockEntityTickOrderWithChangedEntities(t *testing.T) {
	registry := NewBlockRegistry()
	registry.RegisterBlockState(BlockState{Name: "test:be_ticker", Properties: map[string]any{}})
	registry.RegisterBlock(beTickTicker{})
	registry.RegisterBlockState(BlockState{Name: "test:be_plain", Properties: map[string]any{}})
	registry.RegisterBlock(beTickPlain{})
	w := Config{Synchronous: true, Blocks: registry}.New()
	defer w.Close()

	p1, p2, p3, p4 := cube.Pos{0, 0, 0}, cube.Pos{1, 0, 0}, cube.Pos{2, 0, 0}, cube.Pos{3, 0, 0}
	runWorld(w, func(tx *Tx) {
		tx.SetBlock(p1, beTickPlain{}, nil)
		tx.SetBlock(p2, beTickTicker{ID: 2, Converts: []beTickConvert{{p1, 10}, {p3, 30}}}, nil)
		tx.SetBlock(p3, beTickPlain{}, nil)
		tx.SetBlock(p4, beTickTicker{ID: 4}, nil)
	})
	beTickLog = nil
	w.AdvanceTick()
	// p1 was created before the ticker at p2, so the list had passed it; p3 comes after p2 and before p4.
	if want := []int{2, 30, 4}; !slices.Equal(beTickLog, want) {
		t.Fatalf("first tick ticked %v, want %v", beTickLog, want)
	}
	beTickLog = nil
	w.AdvanceTick()
	// p2 was set again as the same block entity, which keeps its place; p1 and p3 come last now.
	if want := []int{2, 4, 10, 30}; !slices.Equal(beTickLog, want) {
		t.Fatalf("second tick ticked %v, want %v", beTickLog, want)
	}
}
