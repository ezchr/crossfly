package playerdb

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// TestTimeSinceRestSaved: the player's time since rest (phantom insomnia)
// survives Data, the JSON provider format and loading it back.
func TestTimeSinceRestSaved(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	var conf player.Config
	_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		id := uuid.New()
		h := world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 0, 0.5}, ID: id}.New(player.Type, player.Config{
			Name: "sleepy", UUID: id, Position: mgl64.Vec3{0.5, 0, 0.5}, TimeSinceRest: 80000,
		})
		p := tx.AddEntity(h).(*player.Player)
		if got := p.TimeSinceRest(); got != 80000 {
			t.Errorf("TimeSinceRest after spawning = %d, want 80000", got)
		}
		p.SetTimeSinceRest(123456)
		conf = p.Data()
		return struct{}{}, nil
	})
	if conf.TimeSinceRest != 123456 {
		t.Fatalf("Data().TimeSinceRest = %d, want 123456", conf.TimeSinceRest)
	}
	prov := &Provider{}
	raw, err := json.Marshal(prov.toJson(conf, w))
	if err != nil {
		t.Fatal(err)
	}
	var d jsonData
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	loaded, _ := prov.fromJson(d, func(world.Dimension) *world.World { return w })
	if loaded.TimeSinceRest != 123456 {
		t.Fatalf("loaded TimeSinceRest = %d, want 123456", loaded.TimeSinceRest)
	}
}
