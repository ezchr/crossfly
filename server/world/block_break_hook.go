package world

import "github.com/df-mc/dragonfly/server/block/cube"

// BlockBreakHook is told that the block old at pos was broken: it is no
// longer in the world when the hook runs. breaker is the entity that caused
// it (the player that broke it, the entity that exploded), nil when none is
// known. Crossfly Full's mob block sensors (piglins and gold, bees and their
// hive) listen with one, like BDS Dimension::onBlockChanged ->
// BlockEventDispatcher::dispatchEvent.
type BlockBreakHook func(tx *Tx, pos cube.Pos, old Block, breaker Entity)

var blockBreakHooks []BlockBreakHook

// RegisterBlockBreakHook adds a hook run by BlockBroken. Call it from an init
// function only: the list is read without locks by every world.
func RegisterBlockBreakHook(h BlockBreakHook) {
	if h == nil {
		panic("world: nil block break hook")
	}
	blockBreakHooks = append(blockBreakHooks, h)
}

// BlockBroken runs the block break hooks for the block old that breaker broke
// at pos. Code that breaks blocks (players, explosions) calls it right after
// the block was removed.
func BlockBroken(tx *Tx, pos cube.Pos, old Block, breaker Entity) {
	for _, h := range blockBreakHooks {
		h(tx, pos, old, breaker)
	}
}

// BlockBreakHooks returns how many block break hooks are registered, so that
// callers can skip work only the hooks need.
func BlockBreakHooks() int { return len(blockBreakHooks) }
