//go:build crossfly_full

package jfarm

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// breakParticle is level event 2001 (LevelEvent.PARTICLES_DESTROY_BLOCK): break particles and sound.
func breakParticle(b world.Block) world.Particle { return particle.BlockBreak{Block: b} }

// dropItem is Block.popResource: the stack as an item at a random spot in the block, with a small random push.
func dropItem(tx *world.Tx, pos cube.Pos, s item.Stack) {
	if s.Empty() {
		return
	}
	at := mgl64.Vec3{float64(pos[0]) + 0.25 + rand.Float64()*0.5, float64(pos[1]) + 0.25 + rand.Float64()*0.5,
		float64(pos[2]) + 0.25 + rand.Float64()*0.5}
	opts := world.EntitySpawnOpts{Position: at, Velocity: mgl64.Vec3{rand.Float64()*0.2 - 0.1, 0.2, rand.Float64()*0.2 - 0.1}}
	tx.AddEntity(tx.World().EntityRegistry().Config().Item(opts, s))
}

// turtleEggSound is the crack or hatch sound of turtle eggs (the Bedrock names the egg block plays too;
// javasession maps them to block.turtle_egg.crack / hatch).
func turtleEggSound(kind string) world.Sound {
	name := "block.turtle_egg.crack"
	if kind == "hatch" {
		name = "mob.turtle_baby.born"
	}
	return sound.Custom{Name: name, Volume: 1, Pitch: 1}
}
