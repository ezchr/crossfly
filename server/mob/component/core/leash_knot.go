//go:build crossfly_full

package core

import (
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// KnotType is the world.EntityType of the fence knot leads are tied to
// (BDS LeashFenceKnotActor, minecraft:leash_knot): it sits in a fence at
// (x+0.5, y+0.25, z+0.5) with a 0.375 x 0.5 box, goes when it is hit, used
// or its fence is gone (checked every 100 ticks, HangingActor::normalTick),
// and when the last mob tied to it is unleashed. Every mob tied to a knot
// that goes drops its lead. It is saved with the world, so mobs tied to it
// find it again by its UUID.
var KnotType knotType

type knotType struct{}

func (knotType) Open(tx *world.Tx, h *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Knot{Ent: entity.Open(tx, h, data)}
}

func (knotType) EncodeEntity() string { return "minecraft:leash_knot" }

func (knotType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.1875, -0.125, -0.1875, 0.1875, 0.375, 0.1875)
}

func (knotType) DecodeNBT(_ map[string]any, data *world.EntityData) { knotConfig{}.Apply(data) }

func (knotType) EncodeNBT(*world.EntityData) map[string]any { return map[string]any{} }

type knotConfig struct{}

func (knotConfig) Apply(data *world.EntityData) { data.Data = &knotBehaviour{} }

// Knot is a fence knot.
type Knot struct {
	*entity.Ent
}

func (k *Knot) behaviour() *knotBehaviour { return k.Behaviour().(*knotBehaviour) }

func (k *Knot) removing() bool { return k.behaviour().removed }

// Interact removes the knot (its interact text is "Remove"); the mobs tied
// to it drop their leads.
func (k *Knot) Interact(_ item.User, held item.Stack, _ mgl64.Vec3) (item.Stack, bool) {
	k.remove(true)
	return held, true
}

// remove is LeashFenceKnotActor::remove: the knot goes and every mob tied to
// it drops its leash (removeAnimals).
func (k *Knot) remove(withSound bool) {
	b := k.behaviour()
	if b.removed {
		return
	}
	b.removed = true
	tx := k.Tx()
	for _, m := range leashedTo(tx, k) {
		DropLeash(m, true, false)
	}
	if withSound {
		playKnotBreak(tx, k.Position())
	}
	_ = k.Close()
}

func playKnotBreak(tx *world.Tx, pos mgl64.Vec3) {
	tx.PlaySound(pos, soundKnotBreak)
}

var soundKnotBreak = sound.Custom{Name: "leashknot.break"}

// knotBehaviour is the entity.Behaviour of a knot.
type knotBehaviour struct {
	check   int
	removed bool
}

// Tick checks every 100 ticks that the knot is still in a fence.
func (b *knotBehaviour) Tick(e *entity.Ent, tx *world.Tx) *entity.Movement {
	b.check++
	if b.check <= 100 || b.removed {
		return nil
	}
	b.check = 0
	if !isFence(tx.Block(cube.PosFromVec3(e.Position()))) {
		if k, ok := knotOf(tx, e.H()); ok {
			k.remove(true)
		}
	}
	return nil
}

// Hurt removes the knot (HangingActor::_hurt).
func (b *knotBehaviour) Hurt(e *entity.Ent, _ float64, _ world.DamageSource) (float64, bool) {
	if k, ok := knotOf(e.Tx(), e.H()); ok {
		k.remove(true)
	}
	return 0, true
}

// Explode removes the knot.
func (b *knotBehaviour) Explode(e *entity.Ent, _ world.ExplosionSource, _ float64) {
	if k, ok := knotOf(e.Tx(), e.H()); ok {
		k.remove(true)
	}
}

// Immobile keeps the knot where it is.
func (b *knotBehaviour) Immobile() bool { return true }

func knotOf(tx *world.Tx, h *world.EntityHandle) (*Knot, bool) {
	if tx == nil {
		return nil, false
	}
	e, ok := h.Entity(tx)
	if !ok {
		return nil, false
	}
	k, ok := e.(*Knot)
	return k, ok
}

// knotAt returns the knot in the fence at pos (findKnotAt).
func knotAt(tx *world.Tx, pos cube.Pos) *Knot {
	x, y, z := float64(pos[0]), float64(pos[1]), float64(pos[2])
	for e := range tx.EntitiesWithin(cube.Box(x, y, z, x+1, y+1, z+1)) {
		if k, ok := e.(*Knot); ok && !k.removing() && cube.PosFromVec3(k.Position()) == pos {
			return k
		}
	}
	return nil
}

// newKnot puts a knot in the fence at pos.
func newKnot(tx *world.Tx, pos cube.Pos) *Knot {
	at := mgl64.Vec3{float64(pos[0]) + 0.5, float64(pos[1]) + 0.25, float64(pos[2]) + 0.5}
	k, _ := tx.AddEntity(world.EntitySpawnOpts{Position: at}.New(KnotType, knotConfig{})).(*Knot)
	return k
}

// isFence reports whether b is a fence leads tie to (any *_fence block).
func isFence(b world.Block) bool {
	if b == nil {
		return false
	}
	name, _ := b.EncodeBlock()
	return strings.HasSuffix(name, "_fence")
}
