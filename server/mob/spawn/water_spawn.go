//go:build crossfly_full

package spawn

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/go-gl/mathgl/mgl64"
)

// The water mobs' checkSpawnRules overrides: the classes whose vtable has
// WaterAnimal::checkSpawnRules in BDS 1.21.3 (Fish = cod, Salmon,
// Pufferfish, TropicalFish, Dolphin, Tadpole) and Squid::checkSpawnRules
// (Squid, GlowSquid).
func init() {
	for _, id := range []string{"minecraft:cod", "minecraft:salmon", "minecraft:pufferfish", "minecraft:tropicalfish", "minecraft:dolphin", "minecraft:tadpole"} {
		actorSpawnRules[id] = waterAnimalSpawnRules
	}
	actorSpawnRules["minecraft:squid"] = squidSpawnRules
	actorSpawnRules["minecraft:glow_squid"] = squidSpawnRules
}

// waterAnimalSpawnRules is WaterAnimal::checkSpawnRules: 1-4 blocks below
// the water surface.
func waterAnimalSpawnRules(e *env, t *mob.Type, pos mgl64.Vec3) (mgl64.Vec3, bool) {
	return waterSpawnRules(e, t, pos, 1)
}

// squidSpawnRules is Squid::checkSpawnRules: 2-5 blocks below the surface.
func squidSpawnRules(e *env, t *mob.Type, pos mgl64.Vec3) (mgl64.Vec3, bool) {
	return waterSpawnRules(e, t, pos, 2)
}

// waterSpawnRules is the body of WaterAnimal/Squid::checkSpawnRules: from
// the spawn block the mob goes up to the highest water block of the column,
// then below+random(0..3) blocks down, never below the block it started in,
// to the block's corner (x and z of the block position, as BDS sets it).
// It may spawn there if no solid block is in its box, its box holds water
// and the block at its feet is a water source. Mob::checkSpawnRules (the
// magma block test) is not asked.
func waterSpawnRules(e *env, t *mob.Type, pos mgl64.Vec3, below int) (mgl64.Vec3, bool) {
	tx := e.tx
	bp := cube.PosFromVec3(pos)
	top := bp[1]
	for top+1 < e.rng[1] && isWater(tx, cube.Pos{bp[0], top + 1, bp[2]}) {
		top++
	}
	y := max(top-e.rnd.IntN(4)-below, int(pos[1]))
	at := mgl64.Vec3{float64(bp[0]), float64(y), float64(bp[2])}
	ti := infoForType(t)
	box := cube.Box(at[0]-ti.w/2, at[1], at[2]-ti.w/2, at[0]+ti.w/2, at[1]+ti.h, at[2]+ti.w/2).Grow(-1e-4)
	for _, bb := range entity.BlockBBoxesAround(tx, box) {
		if bb.IntersectsWith(box) {
			return at, false // BlockSource::containsAnySolidBlocking
		}
	}
	water := false
	for p := range cube.Range3D(cube.PosFromVec3(box.Min()), cube.PosFromVec3(box.Max())) {
		if isWater(tx, p) {
			water = true
			break
		}
	}
	if !water {
		return at, false
	}
	// BlockUtils::isLiquidSource of the block (not the liquid layer).
	w, ok := tx.Block(cube.Pos{bp[0], y, bp[2]}).(block.Water)
	return at, ok && w.Depth == 8 && !w.Falling
}
