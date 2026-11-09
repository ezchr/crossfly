package mcdb

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/goleveldb/leveldb"
)

// An entity that walks from one chunk into another is stored with the new chunk, whichever of the two chunks
// is saved first.
func TestEntityMovedToOtherChunkSurvivesSave(t *testing.T) {
	for _, newFirst := range []bool{false, true} {
		db, err := Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		a, b := dbKey{pos: world.ChunkPos{0, 0}, dim: world.Overworld}, dbKey{pos: world.ChunkPos{1, 0}, dim: world.Overworld}
		store := func(k dbKey, x float32, with bool) {
			var ents []chunk.Entity
			if with {
				ents = []chunk.Entity{{ID: 7, Data: map[string]any{"identifier": "minecraft:cow", "Pos": []float32{x, 64, 2}}}}
			}
			batch := new(leveldb.Batch)
			db.storeEntities(batch, k, ents)
			if err := db.ldb.Write(batch, nil); err != nil {
				t.Fatal(err)
			}
		}
		store(a, 3, true)
		if newFirst {
			store(b, 18, true)
			store(a, 0, false)
		} else {
			store(a, 0, false)
			store(b, 18, true)
		}
		ents, err := db.entities(b)
		if err != nil {
			t.Fatal(err)
		}
		if len(ents) != 1 || ents[0].ID != 7 {
			t.Errorf("new chunk saved first=%v: entities of the new chunk %v, want the cow", newFirst, ents)
		}
		_ = db.Close()
	}
}
