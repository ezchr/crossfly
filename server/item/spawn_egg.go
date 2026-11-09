//go:build crossfly_full

package item

import (
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// SpawnEgg is a spawn egg: used on a block it spawns a mob of its type, used on a mob of its type it
// spawns a baby of that mob.
type SpawnEgg struct {
	// Mob is the entity identifier of the mob the egg spawns, e.g. "minecraft:pig".
	Mob string
}

// MobSpawner spawns the mobs of spawn eggs. Package mob implements it; RegisterSpawnEggs installs it.
type MobSpawner interface {
	// SpawnFromEgg spawns a mob of the identifier at pos, turned towards the point towards (the user), named
	// nameTag if it is not empty. It reports whether a mob was spawned.
	SpawnFromEgg(tx *world.Tx, mob string, pos, towards mgl64.Vec3, nameTag string) bool
	// SpawnBabyFromEgg spawns a baby of the identifier where target is, if target is a mob of that
	// identifier that has babies. It reports whether a baby was spawned.
	SpawnBabyFromEgg(tx *world.Tx, mob string, target world.Entity, nameTag string) bool
}

// mobSpawner is the MobSpawner of the spawn eggs, set by RegisterSpawnEggs.
var mobSpawner MobSpawner

// UseOnBlock spawns the egg's mob on the face of the block clicked, as BDS's ActorPlacerItem: in the
// middle of the block next to the face, half a block higher on top of a fence.
func (s SpawnEgg) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	if mobSpawner == nil {
		return false
	}
	at := pos.Side(face)
	spawnPos := mgl64.Vec3{float64(at[0]) + 0.5, float64(at[1]), float64(at[2]) + 0.5}
	if face == cube.FaceUp && fence(tx.Block(pos)) {
		spawnPos[1] += 0.5
	}
	// BDS turns the mob towards the low corner of the user's bounding box.
	towards := user.H().Type().BBox(user).Translate(user.Position()).Min()
	if !mobSpawner.SpawnFromEgg(tx, s.Mob, spawnPos, towards, s.heldName(user)) {
		return false
	}
	ctx.SubtractFromCount(1)
	return true
}

// UseOnEntity spawns a baby of the egg's mob when used on a mob of the same type, as Java Edition's
// SpawnEggItem.spawnOffspringFromSpawnEgg.
func (s SpawnEgg) UseOnEntity(e world.Entity, tx *world.Tx, user User, ctx *UseContext) bool {
	if mobSpawner == nil || !mobSpawner.SpawnBabyFromEgg(tx, s.Mob, e, s.heldName(user)) {
		return false
	}
	ctx.SubtractFromCount(1)
	return true
}

// heldName is the custom name of the egg the user holds, which the mob it spawns is named after.
func (s SpawnEgg) heldName(user User) string {
	if held, _ := user.HeldItems(); held.Item() == s {
		return held.CustomName()
	}
	return ""
}

// fence reports whether b is a fence (BDS's wood fences and the nether brick fence, which are all the
// fences): a mob spawned on top of one stands on its 1.5 block high collision box.
func fence(b world.Block) bool {
	name, _ := b.EncodeBlock()
	return strings.HasSuffix(name, "_fence")
}

// EncodeItem ...
func (s SpawnEgg) EncodeItem() (name string, meta int16) {
	return spawnEggName(s.Mob), 0
}

// spawnEggName is the name of the spawn egg item of a mob: "minecraft:<mob>_spawn_egg", except for the
// mobs whose entity identifier differs from the egg's.
func spawnEggName(mob string) string {
	switch mob {
	case "minecraft:evocation_illager":
		return "minecraft:evoker_spawn_egg"
	case "minecraft:tropicalfish":
		return "minecraft:tropical_fish_spawn_egg"
	case "minecraft:villager_v2":
		return "minecraft:villager_spawn_egg"
	case "minecraft:zombie_villager_v2":
		return "minecraft:zombie_villager_spawn_egg"
	}
	return mob + "_spawn_egg"
}

// SpawnEggMob reports whether a mob gets a spawn egg, and if so which. It gets none if vanilla Bedrock
// has no egg item for it, or if its egg spawns another identifier: the old villager and zombie villager
// (the eggs spawn the _v2 ones). The agent and the NPC get none either: their eggs need Education
// Edition's Code Builder and the NPC editor, which Crossfly does not have, and Java Edition has
// no item for them.
func SpawnEggMob(mob string) (SpawnEgg, bool) {
	switch mob {
	case "minecraft:villager", "minecraft:zombie_villager", "minecraft:agent", "minecraft:npc":
		return SpawnEgg{}, false
	}
	egg := SpawnEgg{Mob: mob}
	if _, _, ok := world.ItemRuntimeID(egg); !ok {
		return SpawnEgg{}, false
	}
	return egg, true
}
