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

// TestWardenThreatSaved: the warden warning level of sculk shriekers, its cooldown and decrease timer
// survive Data, the JSON provider format and loading it back.
func TestWardenThreatSaved(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	var conf player.Config
	_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		id := uuid.New()
		h := world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 0, 0.5}, ID: id}.New(player.Type, player.Config{
			Name: "Steve", UUID: id, Position: mgl64.Vec3{0.5, 0, 0.5}, WardenThreat: [3]int{2, 150, 9000},
		})
		p := tx.AddEntity(h).(*player.Player)
		if l, c, d := p.WardenThreat(); l != 2 || c != 150 || d != 9000 {
			t.Errorf("WardenThreat after spawning = %d %d %d, want 2 150 9000", l, c, d)
		}
		p.SetWardenThreat(3, 200, 12000)
		conf = p.Data()
		return struct{}{}, nil
	})
	prov := &Provider{}
	raw, err := json.Marshal(prov.toJson(conf, w))
	if err != nil {
		t.Fatal(err)
	}
	var d jsonData
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	if d.WardenThreatLevel != 3 || d.WardenThreatLevelIncreaseCooldown != 200 || d.WardenThreatDecreaseTimer != 12000 {
		t.Errorf("saved %d %d %d, want 3 200 12000", d.WardenThreatLevel, d.WardenThreatLevelIncreaseCooldown, d.WardenThreatDecreaseTimer)
	}
	loaded, _ := prov.fromJson(d, func(world.Dimension) *world.World { return w })
	if loaded.WardenThreat != [3]int{3, 200, 12000} {
		t.Fatalf("loaded WardenThreat = %v, want [3 200 12000]", loaded.WardenThreat)
	}
}
