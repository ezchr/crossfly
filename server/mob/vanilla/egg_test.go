//go:build crossfly_full

package vanilla

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/java/javamap"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// registerEggs registers the vanilla mobs and their spawn eggs.
func registerEggs(t *testing.T) []item.SpawnEgg {
	t.Helper()
	existing := map[string]bool{}
	for _, et := range entity.DefaultRegistry.Types() {
		existing[et.EncodeEntity()] = true
	}
	if _, err := Register(Options{Skip: func(id string) bool { return existing[id] }}); err != nil {
		t.Fatal(err)
	}
	item.RegisterSpawnEggs(mob.Identifiers(), mob.EggSpawner{})
	var eggs []item.SpawnEgg
	for _, id := range mob.Identifiers() {
		if egg, ok := item.SpawnEggMob(id); ok {
			eggs = append(eggs, egg)
		}
	}
	return eggs
}

// TestSpawnEggPerMob checks that every registered mob but the four without one has a registered spawn
// egg, which Java clients see as their own spawn egg.
func TestSpawnEggPerMob(t *testing.T) {
	eggs := registerEggs(t)
	without := map[string]bool{"minecraft:villager": true, "minecraft:zombie_villager": true, "minecraft:agent": true, "minecraft:npc": true}
	byMob := map[string]bool{}
	for _, egg := range eggs {
		byMob[egg.Mob] = true
		name, meta := egg.EncodeItem()
		it, ok := world.ItemByName(name, meta)
		if !ok || it != world.Item(egg) {
			t.Errorf("%v: egg %v not registered", egg.Mob, name)
		}
		if _, ok := javamap.ItemID(name, meta); !ok {
			t.Errorf("%v: egg %v has no Java item", egg.Mob, name)
		}
	}
	for _, id := range mob.Identifiers() {
		if !byMob[id] && !without[id] {
			t.Errorf("%v has no spawn egg", id)
		}
		if byMob[id] && without[id] {
			t.Errorf("%v has a spawn egg", id)
		}
	}
	if len(eggs) != len(mob.Identifiers())-len(without) {
		t.Errorf("%d eggs for %d mobs", len(eggs), len(mob.Identifiers()))
	}
	for id, want := range map[string]string{
		"minecraft:pig":                "minecraft:pig_spawn_egg",
		"minecraft:mooshroom":          "minecraft:mooshroom_spawn_egg",
		"minecraft:evocation_illager":  "minecraft:evoker_spawn_egg",
		"minecraft:tropicalfish":       "minecraft:tropical_fish_spawn_egg",
		"minecraft:villager_v2":        "minecraft:villager_spawn_egg",
		"minecraft:zombie_villager_v2": "minecraft:zombie_villager_spawn_egg",
		"minecraft:zombie_pigman":      "minecraft:zombie_pigman_spawn_egg",
	} {
		if name, _ := (item.SpawnEgg{Mob: id}).EncodeItem(); name != want {
			t.Errorf("%v: egg %v, want %v", id, name, want)
		}
	}
}

// TestSpawnEggUse uses spawn eggs as a player: on a block, on top of a fence, on a mob of the egg's
// type (a baby), in survival (an egg is used up) and in creative (none is).
func TestSpawnEggUse(t *testing.T) {
	registerEggs(t)
	reg := entity.DefaultRegistry.Config().New(append(entity.DefaultRegistry.Types(), mob.Types()...))
	w := world.Config{Entities: reg, Provider: world.NopProvider{}, Generator: world.NopGenerator{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}.New()
	defer w.Close()
	_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		for x := -8; x <= 8; x++ {
			for z := -8; z <= 8; z++ {
				tx.SetBlock(cube.Pos{x, 9, z}, block.Stone{}, nil)
			}
		}
		tx.SetBlock(cube.Pos{2, 10, 0}, block.WoodFence{Wood: block.OakWood()}, nil)
		pos := mgl64.Vec3{0.5, 10, 3.5}
		h := world.EntitySpawnOpts{Position: pos}.New(player.Type, player.Config{Name: "p", Position: pos, GameMode: world.GameModeSurvival})
		p := tx.AddEntity(h).(*player.Player)
		pigEgg := item.SpawnEgg{Mob: "minecraft:pig"}
		p.SetHeldItems(item.NewStack(pigEgg, 3).WithCustomName("Egbert"), item.Stack{})

		mobsOf := func(id string) []*mob.Mob {
			var out []*mob.Mob
			for e := range tx.Entities() {
				if m, ok := mob.Of(e); ok && m.Identifier() == id {
					out = append(out, m)
				}
			}
			return out
		}
		held := func() int { s, _ := p.HeldItems(); return s.Count() }

		p.UseItemOnBlock(cube.Pos{0, 9, 0}, cube.FaceUp, mgl64.Vec3{0.5, 10, 0.5})
		pigs := mobsOf("minecraft:pig")
		if len(pigs) != 1 {
			t.Fatalf("%d pigs after using the egg on stone, want 1", len(pigs))
		}
		pig := pigs[0]
		if got := pig.Position(); got != (mgl64.Vec3{0.5, 10, 0.5}) {
			t.Errorf("pig at %v, want on top of the stone at 0.5 10 0.5", got)
		}
		// The egg fires minecraft:entity_spawned, so like any spawned pig it
		// is a baby 1 time in 20 (pig.json randomize); that is not checked.
		if pig.NameTag() != "Egbert" || !pig.Persistent() {
			t.Errorf("pig: name %q, persistent %v", pig.NameTag(), pig.Persistent())
		}
		// The player is south of the pig: it looks south (yaw 0).
		if yaw := pig.Rotation().Yaw(); yaw < -30 || yaw > 30 {
			t.Errorf("pig yaw %v, want facing the player (about 0)", yaw)
		}
		if n := held(); n != 2 {
			t.Errorf("%d eggs left in survival, want 2", n)
		}

		p.UseItemOnBlock(cube.Pos{2, 10, 0}, cube.FaceUp, mgl64.Vec3{2.5, 11.5, 0.5})
		var onFence *mob.Mob
		for _, m := range mobsOf("minecraft:pig") {
			if m != pig && m.H() != pig.H() {
				onFence = m
			}
		}
		if onFence == nil {
			t.Fatal("no pig on the fence")
		}
		if got := onFence.Position(); got != (mgl64.Vec3{2.5, 11.5, 0.5}) {
			t.Errorf("pig on the fence at %v, want 2.5 11.5 0.5", got)
		}
		if n := held(); n != 1 {
			t.Errorf("%d eggs left in survival, want 1", n)
		}

		babies := func() (n int) {
			for _, m := range mobsOf("minecraft:pig") {
				if m.Baby() {
					n++
				}
			}
			return n
		}
		p.SetGameMode(world.GameModeCreative)
		before := babies()
		if !p.UseItemOnEntity(pig) {
			t.Fatal("using the egg on the pig failed")
		}
		if n := babies() - before; n != 1 {
			t.Errorf("%d new baby pigs after using the egg on a pig, want 1", n)
		}
		if n := held(); n != 1 {
			t.Errorf("%d eggs left in creative, want 1", n)
		}

		// An egg of another mob does nothing to the pig.
		p.SetHeldItems(item.NewStack(item.SpawnEgg{Mob: "minecraft:cow"}, 1), item.Stack{})
		cows := len(mobsOf("minecraft:cow"))
		p.UseItemOnEntity(pig)
		if len(mobsOf("minecraft:cow")) != cows {
			t.Error("a cow egg on a pig spawned a cow")
		}
		return struct{}{}, nil
	})
}
