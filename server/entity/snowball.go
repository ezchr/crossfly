package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
)

// NewSnowball creates a snowball entity at a position with an owner entity.
func NewSnowball(opts world.EntitySpawnOpts, owner world.Entity) *world.EntityHandle {
	conf := snowballConf
	conf.Owner = ownerHandle(owner)
	return opts.New(SnowballType, conf)
}

var snowballConf = ProjectileBehaviourConfig{
	Gravity:       0.03,
	Drag:          0.01,
	Particle:      particle.SnowballPoof{},
	ParticleCount: 6,
	Hit:           snowballHit,
}

// snowballHit is the snowball impact_damage of the Bedrock definition
// (damage 3, filter blaze): a snowball hurts only blazes, by 3. Every other
// entity hit takes the 0 damage hit and the knock back of the projectile.
func snowballHit(e *Ent, tx *world.Tx, res trace.Result) {
	r, ok := res.(trace.EntityResult)
	if !ok || r.Entity().H().Type().EncodeEntity() != "minecraft:blaze" {
		return
	}
	l, ok := r.Entity().(Living)
	if !ok {
		return
	}
	var owner world.Entity
	if h := e.ProjectileOwner(); h != nil {
		owner, _ = h.Entity(tx)
	}
	l.Hurt(3, ProjectileDamageSource{Projectile: e, Owner: owner})
}

// SnowballType is a world.EntityType implementation for snowballs.
var SnowballType snowballType

type snowballType struct{}

func (t snowballType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Ent{tx: tx, handle: handle, data: data}
}

func (snowballType) EncodeEntity() string { return "minecraft:snowball" }
func (snowballType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.125, 0, -0.125, 0.125, 0.25, 0.125)
}

func (snowballType) DecodeNBT(_ map[string]any, data *world.EntityData) {
	data.Data = snowballConf.New()
}
func (snowballType) EncodeNBT(*world.EntityData) map[string]any { return nil }
