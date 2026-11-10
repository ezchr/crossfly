//go:build crossfly_full

package block

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/gameevent"
)

// Dispense primes TNT at the block in front of a dispenser.
func (TNT) Dispense(pos cube.Pos, face cube.Face, tx *world.Tx, ctx *item.DispenseContext) item.DispenseResult {
	if tx.World().EntityRegistry().Config().TNT == nil {
		return item.DispenseFailure
	}
	front := pos.Side(face)
	b := tx.Block(front)
	if _, air := b.(Air); !air {
		replaceable, ok := b.(Replaceable)
		if !ok || !replaceable.ReplaceableBy(TNT{}) {
			return item.DispenseFailure
		}
	}
	ctx.SubtractFromCount(1)
	spawnTnt(front, tx, world.TNTSpawnConfig{Fuse: time.Second * 4})
	// TntBlock::dispense: entity_place where the primed TNT appears.
	postBlockEvent(tx, gameevent.EntityPlace, front, nil)
	return item.DispenseSuccess
}

// With Java redstone, TNT has no RedstonePowerAction: as Java Edition's TntBlock, it only looks at its power when it
// is placed and when a neighbour updates it (neighborChanged). Power that reaches it without an update, as when dust a
// piston uncovers changes shape (a shape update notifies nobody), does not prime it until something updates it: TNT
// dupers rely on that, the piston taking the TNT along while it primes.

// BlockPlaced primes TNT placed where it is powered, as Java Edition's onPlace.
func (t TNT) BlockPlaced(pos cube.Pos, previous world.Block, tx *world.Tx) {
	if _, same := previous.(TNT); !same && railNeighbourSignal(tx, pos) {
		t.primeByRedstone(pos, tx)
	}
}

// NeighbourUpdateTick primes TNT that is powered when a neighbour changes, as Java Edition's neighborChanged.
func (t TNT) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if world.JavaRedstone() && railNeighbourSignal(tx, pos) {
		t.primeByRedstone(pos, tx)
	}
}

func (t TNT) primeByRedstone(pos cube.Pos, tx *world.Tx) {
	if _, ok := tx.Block(pos).(TNT); !ok {
		return
	}
	spawnTnt(pos, tx, world.TNTSpawnConfig{Fuse: time.Second * 4, UnblockableByShield: true})
	postBlockEvent(tx, gameevent.PrimeFuse, pos, nil)
}
