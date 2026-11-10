//go:build crossfly_full

package jfarm

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

const (
	// TurtleLayEggTicks is TurtleLayEggGoal's adjustedTickDelay(200) goal ticks of digging (200 game ticks)
	// before the eggs are placed.
	TurtleLayEggTicks = 200
	// TurtleScuteItem is the gift loot table gameplay/turtle_grow: one turtle scute when a baby grows up
	// (Turtle.ageBoundaryReached, with the mobDrops rule).
	TurtleScuteItem = "turtle_scute"
	// TurtleBabyScale is Turtle.BABY_SCALE.
	TurtleBabyScale = 0.3
	// TurtleHomeRadius: TurtleGoHomeGoal starts 1 in 350 goal ticks (reducedTickDelay(700)) when farther than
	// 64 blocks from home, and always when the turtle carries eggs; it ends within 7 blocks.
	TurtleHomeRadius = 64
)

// TurtleEggCount is the cluster TurtleLayEggGoal places: 1 to 4 eggs (random.nextInt(4) + 1).
func TurtleEggCount(r *rand.Rand) int { return r.IntN(4) + 1 }

// IsSand is the block tag minecraft:sand (sand, red sand, suspicious sand): TurtleEggBlock.isSand.
func IsSand(b world.Block) bool {
	if b == nil {
		return false
	}
	name, _ := b.EncodeBlock()
	switch name {
	case "minecraft:sand", "minecraft:red_sand", "minecraft:suspicious_sand":
		return true
	}
	return false
}

// TurtleEggHatchChance is the environment attribute gameplay/turtle_egg_hatch_chance of the overworld
// (timeline minecraft:day, constant ease): 1.0 from tick 21062 to 21905 of the day (the hour before
// sunrise), 0.002 the rest of the day. Other dimensions have no day timeline and never hatch.
func TurtleEggHatchChance(tx *world.Tx) float64 {
	if tx.World().Dimension() != world.Overworld {
		return 0
	}
	t := (tx.World().Time()%24000 + 24000) % 24000
	if t >= 21062 && t < 21905 {
		return 1
	}
	return 0.002
}

// TurtleEggRandomTick is TurtleEggBlock.randomTick: with the hatch chance an egg on sand cracks, and a
// cluster cracked twice hatches into baby turtles that call the egg position home. It returns the number
// of turtles to spawn (spawn them at TurtleHatchPos with age BabyStartAge and the egg as home).
func TurtleEggRandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand, egg block.TurtleEgg) (hatch int) {
	chance := TurtleEggHatchChance(tx)
	if !(chance > 0 && float64(r.Float32()) < chance) || !IsSand(tx.Block(pos.Side(cube.FaceDown))) {
		return 0
	}
	if egg.Cracks < 2 {
		egg.Cracks++
		tx.SetBlock(pos, egg, nil)
		tx.PlaySound(pos.Vec3Centre(), turtleEggSound("crack"))
		return 0
	}
	tx.PlaySound(pos.Vec3Centre(), turtleEggSound("hatch"))
	tx.SetBlock(pos, nil, nil)
	n := egg.AdditionalCount + 1
	for range n {
		tx.AddParticle(pos.Vec3Centre(), breakParticle(egg))
	}
	return n
}

// TurtleHatchPos is where TurtleEggBlock.randomTick places the i-th baby: x + 0.3 + i * 0.2, z + 0.3.
func TurtleHatchPos(pos cube.Pos, i int) (x, y, z float64) {
	return float64(pos[0]) + 0.3 + float64(i)*0.2, float64(pos[1]), float64(pos[2]) + 0.3
}
