//go:build crossfly_full

package boss

import (
	"log/slog"
	"sync"

	"github.com/df-mc/dragonfly/server/world"
)

// theEndKey is the level-wide database key Bedrock keeps the End fight under
// (SavedDataStorage "TheEnd": {"data": {"DragonFight": {...}}}).
const theEndKey = "TheEnd"

// levelDataProvider is the part of a world.Provider that keeps level-wide
// data (mcdb.DB).
type levelDataProvider interface {
	LevelData(key string) (map[string]any, error)
	SetLevelData(key string, data map[string]any) error
}

// killedDragons remembers the databases (or, for providers that keep no
// level data, the worlds) a dragon died in, so the flag is read from disk
// once.
var killedDragons sync.Map

func dragonKillKey(w *world.World) any {
	if p, ok := w.Provider().(levelDataProvider); ok {
		return p
	}
	return w
}

// DragonPreviouslyKilled is EndDragonFight::hasDragonPreviouslyBeenKilled:
// whether a dragon died in this world before, saved with the world as
// Bedrock saves it (TheEnd, DragonFight.PreviouslyKilled).
func DragonPreviouslyKilled(w *world.World) bool {
	key := dragonKillKey(w)
	if _, ok := killedDragons.Load(key); ok {
		return true
	}
	lp, ok := w.Provider().(levelDataProvider)
	if !ok {
		return false
	}
	data, err := lp.LevelData(theEndKey)
	if err != nil {
		slog.Default().Warn("mob: read the End fight", "err", err)
		return false
	}
	d, _ := data["data"].(map[string]any)
	fight, _ := d["DragonFight"].(map[string]any)
	if v, ok := fight["PreviouslyKilled"].(byte); ok && v != 0 {
		killedDragons.Store(key, true)
		return true
	}
	return false
}

// SetDragonPreviouslyKilled is EndDragonFight::
// setHasDragonPreviouslyBeenKilled(true) with the dragon killed: the flag is
// written to the world's database at once, keeping the rest of the End
// fight's data.
func SetDragonPreviouslyKilled(w *world.World) {
	killedDragons.Store(dragonKillKey(w), true)
	lp, ok := w.Provider().(levelDataProvider)
	if !ok {
		return
	}
	data, err := lp.LevelData(theEndKey)
	if err != nil {
		slog.Default().Warn("mob: read the End fight", "err", err)
		return
	}
	if data == nil {
		data = map[string]any{}
	}
	d, _ := data["data"].(map[string]any)
	if d == nil {
		d = map[string]any{}
		data["data"] = d
	}
	fight, _ := d["DragonFight"].(map[string]any)
	if fight == nil {
		fight = map[string]any{}
		d["DragonFight"] = fight
	}
	fight["PreviouslyKilled"], fight["DragonKilled"] = byte(1), byte(1)
	if err := lp.SetLevelData(theEndKey, data); err != nil {
		slog.Default().Warn("mob: save the End fight", "err", err)
	}
}
