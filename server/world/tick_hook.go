package world

// TickHook is a function run once per world tick, inside the tick's
// transaction, right after the entities were ticked. Packages that need a
// per-world tick (natural mob spawning) register one instead of running a
// ticker of their own. Like the entity tick, hooks of a world that has no
// viewers do not run (synchronous worlds always run them).
type TickHook func(tx *Tx)

var tickHooks []TickHook

// RegisterTickHook adds a hook run in every world each tick. Call it from an
// init function only: the list is read without locks by every world.
func RegisterTickHook(h TickHook) {
	if h == nil {
		panic("world: nil tick hook")
	}
	tickHooks = append(tickHooks, h)
}

// TickHooks returns how many tick hooks are registered. Crossfly Full checks
// with it that features turned off registered none.
func TickHooks() int { return len(tickHooks) }
