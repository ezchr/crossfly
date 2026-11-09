//go:build crossfly_full

package vanilla

import (
	"context"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestSpawnAllMobs registers every vanilla mob, spawns each on a stone floor
// and ticks it for two seconds, with whatever mob packages are linked in. It
// catches panics in components and goals across all 92 definitions.
func TestSpawnAllMobs(t *testing.T) {
	existing := map[string]bool{}
	for _, et := range entity.DefaultRegistry.Types() {
		existing[et.EncodeEntity()] = true
	}
	types, err := Register(Options{Skip: func(id string) bool { return existing[id] }})
	if err != nil {
		t.Fatal(err)
	}
	if len(types) < 80 {
		t.Fatalf("only %d mob types registered", len(types))
	}
	reg := entity.DefaultRegistry.Config().New(append(entity.DefaultRegistry.Types(), mob.Types()...))
	w := world.Config{Entities: reg, Provider: world.NopProvider{}, Generator: world.NopGenerator{}}.New()
	defer w.Close()
	_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		for x := -40; x <= 40; x++ {
			for z := -40; z <= 40; z++ {
				tx.SetBlock(cube.Pos{x, 9, z}, block.Stone{}, nil)
			}
		}
		var hs []*world.EntityHandle
		for i, ty := range types {
			pos := mgl64.Vec3{float64(i%10)*8 - 36, 10, float64(i/10)*8 - 36}
			m, err := mob.Spawn(tx, ty.EncodeEntity(), pos, mob.SpawnOptions{})
			if err != nil {
				t.Errorf("%v: %v", ty.EncodeEntity(), err)
				continue
			}
			hs = append(hs, m.H())
		}
		for tick := int64(0); tick < 40; tick++ {
			for _, h := range hs {
				if m, ok := mob.OfHandle(tx, h); ok {
					m.Tick(tx, tick)
				}
			}
		}
		return struct{}{}, nil
	})
	for k, err := range mob.FactoryErrors() {
		t.Errorf("factory error %v: %v", k, err)
	}
}
