package entity

import (
	"context"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// pearlHit is a trace.Result at a position.
type pearlHit struct{ pos mgl64.Vec3 }

func (h pearlHit) BBox() cube.BBox      { return cube.BBox{} }
func (h pearlHit) Position() mgl64.Vec3 { return h.pos }
func (pearlHit) Face() cube.Face        { return cube.FaceUp }

// countingHost is a block.MobHost that records the mobs it spawns.
type countingHost struct {
	ids  []string
	pos  []mgl64.Vec3
	opts []block.MobSpawnOptions
}

func (c *countingHost) SpawnMob(_ *world.Tx, id string, pos mgl64.Vec3, opts block.MobSpawnOptions) (world.Entity, bool) {
	c.ids, c.pos, c.opts = append(c.ids, id), append(c.pos, pos), append(c.opts, opts)
	return nil, true
}

// TestEnderPearlEndermite: about 5% of pearl hits let out a persistent
// endermite facing the pearl's yaw, lifted onto the collision box of the
// block it landed in.
func TestEnderPearlEndermite(t *testing.T) {
	prev := block.Mobs()
	host := &countingHost{}
	block.SetMobHost(host)
	t.Cleanup(func() { block.SetMobHost(prev) })

	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	pearl := &Ent{data: &world.EntityData{Rot: cube.Rotation{90, 0}}}
	const hits = 4000
	_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		tx.SetBlock(cube.Pos{0, 0, 0}, block.Slab{Block: block.Stone{}}, nil)
		for range hits {
			spawnEndermite(pearl, tx, pearlHit{pos: mgl64.Vec3{0.3, 0.2, 0.7}})
		}
		return struct{}{}, nil
	})
	n := len(host.ids)
	if n < hits*3/100 || n > hits*7/100 {
		t.Fatalf("%d endermites from %d pearl hits, want about 5%%", n, hits)
	}
	for i, id := range host.ids {
		if id != "minecraft:endermite" || !host.opts[i].Persistent || host.opts[i].Yaw != 90 {
			t.Fatalf("spawned %v with %+v", id, host.opts[i])
		}
		if want := (mgl64.Vec3{0.3, 0.5, 0.7}); host.pos[i] != want {
			t.Fatalf("endermite at %v, want on the slab at %v", host.pos[i], want)
		}
	}
}
