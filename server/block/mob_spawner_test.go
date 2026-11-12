//go:build crossfly_full

package block_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestMobSpawnerRegistered checks that the spawner is a block and an item.
func TestMobSpawnerRegistered(t *testing.T) {
	block.RegisterMobBlocks()
	eggWorld(t)
	if b, ok := world.BlockByName("minecraft:mob_spawner", nil); !ok {
		t.Errorf("mob_spawner block: %v %v", b, ok)
	} else if _, ok := b.(block.MobSpawner); !ok {
		t.Errorf("mob_spawner is %T", b)
	}
	if it, ok := world.ItemByName("minecraft:mob_spawner", 0); !ok || it != (block.MobSpawner{}) {
		t.Errorf("mob_spawner item: %v %v", it, ok)
	}
}

// TestMobSpawnerNBT: the block entity carries BDS's keys (EntityIdentifier for the mob in the cage) and reads
// back the same; missing keys get BaseMobSpawner's defaults.
func TestMobSpawnerNBT(t *testing.T) {
	s := block.NewMobSpawner("minecraft:zombie")
	s.Delay, s.SpawnRange = 77, 6
	nbt := s.EncodeNBT()
	if nbt["id"] != "MobSpawner" || nbt["EntityIdentifier"] != "minecraft:zombie" || nbt["Delay"] != int16(77) {
		t.Errorf("NBT %v", nbt)
	}
	if got := (block.MobSpawner{}).DecodeNBT(nbt).(block.MobSpawner); got != s {
		t.Errorf("round trip: %+v, want %+v", got, s)
	}
	d := (block.MobSpawner{}).DecodeNBT(map[string]any{"EntityIdentifier": "minecraft:pig"}).(block.MobSpawner)
	if d != block.NewMobSpawner("minecraft:pig") {
		t.Errorf("defaults: %+v", d)
	}
	if d.MinSpawnDelay != 200 || d.MaxSpawnDelay != 800 || d.SpawnCount != 4 || d.MaxNearbyEntities != 6 ||
		d.RequiredPlayerRange != 16 || d.SpawnRange != 4 || d.Delay != 20 {
		t.Errorf("BDS defaults wrong: %+v", d)
	}
	// A spawner set by a command or placed as an item has no NBT: it saves the defaults.
	if e := (block.MobSpawner{}).EncodeNBT(); e["MaxSpawnDelay"] != int16(800) || e["EntityIdentifier"] != "" {
		t.Errorf("zero spawner NBT %v", e)
	}
}

// TestMobSpawnerBreak: no drops, 15-43 experience, a pickaxe needed.
func TestMobSpawnerBreak(t *testing.T) {
	bi := block.MobSpawner{}.BreakInfo()
	if d := bi.Drops(item.Pickaxe{Tier: item.ToolTierDiamond}, nil); len(d) != 0 {
		t.Errorf("spawner drops %v", d)
	}
	if bi.XPDrops != (block.XPDropRange{15, 43}) {
		t.Errorf("xp %v, want 15-43", bi.XPDrops)
	}
	if !bi.Harvestable(item.Pickaxe{Tier: item.ToolTierWood}) || bi.Harvestable(item.ToolNone{}) {
		t.Error("a spawner should need a pickaxe")
	}
}

type eggTestSpawner struct{}

func (eggTestSpawner) SpawnFromEgg(*world.Tx, string, mgl64.Vec3, mgl64.Vec3, string) bool {
	return true
}
func (eggTestSpawner) SpawnBabyFromEgg(*world.Tx, string, world.Entity, string) bool { return false }

// TestSpawnEggOnSpawner: a spawn egg used on a spawner sets its mob, keeping its settings, and is used up.
func TestSpawnEggOnSpawner(t *testing.T) {
	item.RegisterSpawnEggs(nil, eggTestSpawner{})
	w := eggWorld(t)
	pos := cube.Pos{0, 0, 0}
	eggDo(t, w, func(tx *world.Tx) {
		s := block.NewMobSpawner("minecraft:zombie")
		s.SpawnRange = 7
		tx.SetBlock(pos, s, nil)
		egg := item.SpawnEgg{Mob: "minecraft:skeleton"}
		u := &eggUser{held: item.NewStack(egg, 2), tx: tx}
		ctx := &item.UseContext{}
		if !egg.UseOnBlock(pos, cube.FaceUp, mgl64.Vec3{}, tx, u, ctx) {
			t.Fatal("the egg did nothing on the spawner")
		}
		got, ok := tx.Block(pos).(block.MobSpawner)
		if !ok || got.EntityID != "minecraft:skeleton" || got.SpawnRange != 7 {
			t.Errorf("spawner after the egg: %+v", tx.Block(pos))
		}
		if ctx.CountSub != 1 {
			t.Errorf("the egg was used %d times", ctx.CountSub)
		}
		// A placed spawner (no NBT) gets the defaults with the mob.
		tx.SetBlock(pos, block.MobSpawner{}, nil)
		egg.UseOnBlock(pos, cube.FaceUp, mgl64.Vec3{}, tx, u, &item.UseContext{})
		if got := tx.Block(pos).(block.MobSpawner); got != block.NewMobSpawner("minecraft:skeleton") {
			t.Errorf("placed spawner after the egg: %+v", got)
		}
	})
}
