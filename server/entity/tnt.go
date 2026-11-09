package entity

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// NewTNT creates a new primed TNT entity.
func NewTNT(opts world.EntitySpawnOpts, fuse time.Duration) *world.EntityHandle {
	return NewTNTWithConfig(opts, world.TNTSpawnConfig{Fuse: fuse})
}

// NewTNTWithConfig creates a new primed TNT entity with additional configuration.
func NewTNTWithConfig(opts world.EntitySpawnOpts, conf world.TNTSpawnConfig) *world.EntityHandle {
	if opts.Velocity.Len() == 0 {
		angle := rand.Float64() * math.Pi * 2
		opts.Velocity = mgl64.Vec3{-math.Sin(angle) * 0.02, 0.1, -math.Cos(angle) * 0.02}
		if world.JavaRedstone() {
			// Java's PrimedTnt: 0.02 sideways in a random direction and 0.2 (a float) up.
			opts.Velocity[1] = float64(float32(0.2))
		}
	}
	return opts.New(TNTType, tntBehaviourConfig{TNTSpawnConfig: conf})
}

var tntConf = PassiveBehaviourConfig{
	Gravity: 0.04,
	Drag:    0.02,
}

// tntBehaviourConfig configures a primed TNT entity.
type tntBehaviourConfig struct {
	world.TNTSpawnConfig
}

// Apply stores the configured TNT behaviour in data.
func (conf tntBehaviourConfig) Apply(data *world.EntityData) {
	data.Data = conf.New()
}

// New creates TNT behaviour that explodes after the configured fuse.
func (conf tntBehaviourConfig) New() *tntBehaviour {
	b := &tntBehaviour{source: conf.Source, unblockableByShield: conf.UnblockableByShield}
	confPassive := tntConf
	confPassive.ExistenceDuration = conf.Fuse
	confPassive.Expire = func(e *Ent, tx *world.Tx) {
		if !b.exploded {
			b.exploded = true
			explodeTNT(e, tx, b)
		}
	}
	b.PassiveBehaviour = confPassive.New()
	return b
}

// tntBehaviour carries the source and shield blockability of primed TNT.
type tntBehaviour struct {
	*PassiveBehaviour
	source              *world.EntityHandle
	unblockableByShield bool
	exploded            bool
}

// Tick ticks primed TNT. With Java redstone it ticks as Java Edition's PrimedTnt (tnt_java.go).
func (b *tntBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if world.JavaRedstone() {
		return b.javaTick(e, tx)
	}
	return b.PassiveBehaviour.Tick(e, tx)
}

// explodeTNT creates an explosion for primed TNT, with Java redstone centred 1/16 of its height above its feet as in
// Java Edition.
func explodeTNT(e *Ent, tx *world.Tx, b *tntBehaviour) {
	tntExplosionConfig(tx, b).Explode(tx, world.EntityExplosionSource{
		Entity:        e,
		ExplosionSize: 4,
		OffsetY:       tntExplosionOffset(),
	})
}

// tntExplosionConfig builds a TNT explosion config, resolving source if it is still available.
func tntExplosionConfig(tx *world.Tx, b *tntBehaviour) block.ExplosionConfig {
	var sourceEntity world.Entity
	if b.source != nil {
		sourceEntity, _ = b.source.Entity(tx)
	}
	return block.ExplosionConfig{ItemDropChance: 1, Source: sourceEntity, UnblockableByShield: b.unblockableByShield}
}

// TNTType is a world.EntityType implementation for TNT.
var TNTType tntType

type tntType struct{}

func (t tntType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Ent{tx: tx, handle: handle, data: data}
}

func (tntType) EncodeEntity() string   { return "minecraft:tnt" }
func (tntType) NetworkOffset() float64 { return 0.49 }
func (tntType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.49, 0, -0.49, 0.49, 0.98, 0.49)
}

func (t tntType) DecodeNBT(m map[string]any, data *world.EntityData) {
	data.Data = tntBehaviourConfig{
		TNTSpawnConfig: world.TNTSpawnConfig{
			Fuse:                nbtconv.TickDuration[int16](m, "Fuse"),
			UnblockableByShield: nbtconv.Bool(m, "DragonflyUnblockableByShield"),
		},
	}.New()
}

// EncodeNBT encodes the TNT fuse and non-default shield blockability.
func (tntType) EncodeNBT(data *world.EntityData) map[string]any {
	fuse, unblockableByShield := tntFuseAndUnblockableByShield(data.Data)
	m := map[string]any{"Fuse": int16(min(max(fuse/(time.Second/20), 0), math.MaxInt16))}
	if unblockableByShield {
		m["DragonflyUnblockableByShield"] = uint8(1)
	}
	return m
}

// tntFuseAndUnblockableByShield reads TNT state, treating legacy passive behaviour as shield-blockable.
func tntFuseAndUnblockableByShield(data any) (time.Duration, bool) {
	switch b := data.(type) {
	case *tntBehaviour:
		return b.Fuse(), b.unblockableByShield
	case *PassiveBehaviour:
		return b.Fuse(), false
	default:
		panic("invalid TNT behaviour type")
	}
}

// tntExplosionOffset is how far above primed TNT's feet it explodes: 1/16 of its height in Java Edition.
func tntExplosionOffset() float64 {
	if world.JavaRedstone() {
		return 0.98 * 0.0625
	}
	return 0
}
