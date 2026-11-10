package entity

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

type endCrystalBehaviour struct {
	showBase      bool
	beamTarget    cube.Pos
	hasBeamTarget bool
	explosionSize float64
	// invulnerable keeps the crystal whole whatever hits it, as the crystals the End fight
	// rebuilds on the spikes while the dragon is summoned again.
	invulnerable bool
}

func (b endCrystalBehaviour) Apply(data *world.EntityData) {
	data.Data = b
}

// Tick continuously generates fire at the End crystal's position while in the
// End, if the block at that position is air and the block below it is not air.
func (endCrystalBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if tx.World().Dimension() == world.End {
		pos := cube.PosFromVec3(e.Position())
		if _, air := tx.Block(pos.Side(cube.FaceDown)).(block.Air); !air {
			if _, air := tx.Block(pos).(block.Air); air {
				fire := block.Fire{}.DeriveState(pos, tx)
				tx.SetBlock(pos, fire, nil)
				tx.ScheduleBlockUpdate(pos, fire, time.Duration(30+rand.IntN(10))*time.Second/20)
			}
		}
	}
	return nil
}

// Explode makes the End crystal explode itself when hit by another explosion,
// causing a chain reaction.
func (b endCrystalBehaviour) Explode(e *Ent, _ world.ExplosionSource, impact float64) {
	if impact <= 0 || b.invulnerable {
		return
	}
	explodeEndCrystal(e, b.explosionSize)
}

// Hurt makes the End crystal explode when damaged by any source, even by
// damage that deals no health. Void damage removes it without an explosion.
func (b endCrystalBehaviour) Hurt(e *Ent, damage float64, src world.DamageSource) (float64, bool) {
	damage = max(damage, 0)
	if _, ok := src.(VoidDamageSource); ok {
		_ = e.Close()
		return damage, true
	}
	if b.invulnerable {
		return 0, false
	}
	explodeEndCrystal(e, b.explosionSize)
	return damage, true
}

func (endCrystalBehaviour) Immobile() bool {
	return true
}

func (b endCrystalBehaviour) ShowBase() bool {
	return b.showBase
}

func (b endCrystalBehaviour) BeamTarget() (cube.Pos, bool) {
	return b.beamTarget, b.hasBeamTarget
}

// Invulnerable reports whether nothing but the void destroys the End crystal.
func (b endCrystalBehaviour) Invulnerable() bool {
	return b.invulnerable
}

// SetEndCrystalBeam points the beam of the End crystal e at target, or removes the beam if ok is false, and
// shows the change to viewers. It does nothing if e is not an End crystal.
func SetEndCrystalBeam(e world.Entity, target cube.Pos, ok bool) {
	ent, b, isCrystal := endCrystalOf(e)
	if !isCrystal {
		return
	}
	if b.hasBeamTarget == ok && (!ok || b.beamTarget == target) {
		return
	}
	b.beamTarget, b.hasBeamTarget = target, ok
	if !ok {
		b.beamTarget = cube.Pos{}
	}
	ent.data.Data = b
	ent.updateState()
}

// SetEndCrystalInvulnerable makes the End crystal e withstand every hit and explosion (but the void), or
// not. It does nothing if e is not an End crystal.
func SetEndCrystalInvulnerable(e world.Entity, invulnerable bool) {
	ent, b, isCrystal := endCrystalOf(e)
	if !isCrystal {
		return
	}
	b.invulnerable = invulnerable
	ent.data.Data = b
}

func endCrystalOf(e world.Entity) (*Ent, endCrystalBehaviour, bool) {
	ent, ok := e.(*Ent)
	if !ok {
		return nil, endCrystalBehaviour{}, false
	}
	b, ok := ent.data.Data.(endCrystalBehaviour)
	return ent, b, ok
}

// explodeEndCrystal closes the End crystal and creates a non-incendiary
// explosion at its base, if the crystal was not closed yet.
func explodeEndCrystal(e *Ent, explosionSize float64) {
	if _, ok := e.H().Entity(e.tx); !ok {
		return
	}
	_ = e.Close()
	block.ExplosionConfig{
		SuppressUnderwaterImpact: true,
	}.Explode(e.tx, world.EntityExplosionSource{
		Entity:        e,
		ExplosionSize: explosionSize,
	})
}
