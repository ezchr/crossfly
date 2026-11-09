package entity

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// NewEnderPearl creates an EnderPearl entity. EnderPearl is a smooth, greenish-
// blue item used to teleport.
func NewEnderPearl(opts world.EntitySpawnOpts, owner world.Entity) *world.EntityHandle {
	conf := enderPearlConf
	conf.Owner = owner.H()
	return opts.New(EnderPearlType, conf)
}

var enderPearlConf = ProjectileBehaviourConfig{
	Gravity:  0.03,
	Drag:     0.01,
	Particle: particle.EndermanTeleport{},
	Sound:    sound.Teleport{},
	Hit:      teleport,
}

// teleporter represents a living entity that can teleport.
type teleporter interface {
	// Teleport teleports the entity to the position given.
	Teleport(pos mgl64.Vec3)
	Living
}

// teleport teleports the owner of an Ent to a trace.Result's position. It first may let out an
// endermite (spawnEndermite).
func teleport(e *Ent, tx *world.Tx, target trace.Result) {
	behaviour := e.Behaviour().(*ProjectileBehaviour)
	if behaviour.PortalTravel() {
		return
	}
	spawnEndermite(e, tx, target)
	owner, _ := behaviour.Owner().Entity(tx)
	if user, ok := owner.(teleporter); ok {
		tx.PlaySound(user.Position(), sound.Teleport{})
		user.Teleport(target.Position())
		user.Hurt(5, FallDamageSource{})
	}
}

// endermiteChance is the on_hit spawn_chance of ender_pearl.json (first_spawn_chance 0.05, one
// minecraft:endermite).
const endermiteChance = 0.05

// spawnEndermite is the ender pearl's on_hit spawn_chance (BDS SpawnChanceSubcomponent::doOnHitEffect):
// with a 5% chance a persistent endermite spawns where the pearl landed, on top of the collision box of
// the block there, facing the pearl's yaw. It spawns through block.SpawnMob, so only with mobs.
func spawnEndermite(e *Ent, tx *world.Tx, target trace.Result) {
	if rand.Float32() > endermiteChance {
		return
	}
	pos := target.Position()
	bp := cube.PosFromVec3(pos)
	if boxes := tx.Block(bp).Model().BBox(bp, tx); len(boxes) > 0 {
		top := boxes[0].Max()[1]
		for _, b := range boxes[1:] {
			top = max(top, b.Max()[1])
		}
		pos[1] = float64(bp[1]) + top
	}
	block.SpawnMob(tx, "minecraft:endermite", pos, block.MobSpawnOptions{Yaw: e.Rotation().Yaw(), Persistent: true})
}

// EnderPearlType is a world.EntityType implementation for EnderPearl.
var EnderPearlType enderPearlType

type enderPearlType struct{}

func (t enderPearlType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Ent{tx: tx, handle: handle, data: data}
}

func (enderPearlType) EncodeEntity() string { return "minecraft:ender_pearl" }
func (enderPearlType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.125, 0, -0.125, 0.125, 0.25, 0.125)
}
func (enderPearlType) DecodeNBT(_ map[string]any, data *world.EntityData) {
	data.Data = enderPearlConf.New()
}
func (enderPearlType) EncodeNBT(*world.EntityData) map[string]any { return nil }
