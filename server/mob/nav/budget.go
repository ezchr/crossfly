//go:build crossfly_full

package nav

import (
	"sync"

	"github.com/df-mc/dragonfly/server/world"
)

// Default per-world limits. BDS has no budget; this is a safety valve that changes nothing while
// a world stays under it. A full search is at most 140 expansions, so 1500 is about ten worst-case
// searches per tick.
const (
	DefaultMaxSearches   = 8
	DefaultMaxExpansions = 1500
)

// Budget limits path searches per world tick. Navigation.MoveTo calls that find the budget used up
// are queued and run on a later tick (the navigation reports Pending meanwhile).
//
// A world ticks on one goroutine, but a Budget is safe for concurrent use anyway.
type Budget struct {
	// MaxSearches is the number of searches allowed per tick (<= 0: unlimited).
	MaxSearches int
	// MaxExpansions is the number of node expansions allowed per tick (<= 0: unlimited). A search
	// that starts under the limit is always finished.
	MaxExpansions int

	mu                   sync.Mutex
	tick                 int64
	searches, expansions int
	deferred             int
}

// NewBudget returns a Budget with the given per-tick limits.
func NewBudget(maxSearches, maxExpansions int) *Budget {
	return &Budget{MaxSearches: maxSearches, MaxExpansions: maxExpansions}
}

func (b *Budget) roll(tick int64) {
	if tick != b.tick {
		b.tick, b.searches, b.expansions, b.deferred = tick, 0, 0, 0
	}
}

// TryStart reserves one search in tick. It returns false when the tick's budget is used up.
func (b *Budget) TryStart(tick int64) bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.roll(tick)
	if (b.MaxSearches > 0 && b.searches >= b.MaxSearches) || (b.MaxExpansions > 0 && b.expansions >= b.MaxExpansions) {
		b.deferred++
		return false
	}
	b.searches++
	return true
}

// Spend records the expansions of a finished search.
func (b *Budget) Spend(tick int64, expansions int) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.roll(tick)
	b.expansions += expansions
}

// Count records a search that ran without TryStart (Navigation.CreatePath).
func (b *Budget) Count(tick int64, expansions int) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.roll(tick)
	b.searches++
	b.expansions += expansions
}

// Stats returns the usage of the current tick.
func (b *Budget) Stats() (tick int64, searches, expansions, deferred int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tick, b.searches, b.expansions, b.deferred
}

var worldBudgets sync.Map // *world.World -> *Budget

// WorldBudget returns the shared Budget of w, creating one with the default limits.
func WorldBudget(w *world.World) *Budget {
	if b, ok := worldBudgets.Load(w); ok {
		return b.(*Budget)
	}
	b, _ := worldBudgets.LoadOrStore(w, NewBudget(DefaultMaxSearches, DefaultMaxExpansions))
	return b.(*Budget)
}

// ForgetWorld drops the Budget of a closed world.
func ForgetWorld(w *world.World) { worldBudgets.Delete(w) }
