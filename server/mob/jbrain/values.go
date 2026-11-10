package jbrain

import (
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// GlobalPos is Java's GlobalPos: a block position in a dimension.
type GlobalPos struct {
	Dim world.Dimension
	Pos cube.Pos
}

// String implements fmt.Stringer.
func (g GlobalPos) String() string { return fmt.Sprintf("%v %v", dimName(g.Dim), g.Pos) }

func dimName(d world.Dimension) string {
	switch d {
	case world.Nether:
		return "minecraft:the_nether"
	case world.End:
		return "minecraft:the_end"
	}
	return "minecraft:overworld"
}

func dimByName(n string) (world.Dimension, bool) {
	switch n {
	case "minecraft:overworld", "overworld":
		return world.Overworld, true
	case "minecraft:the_nether", "the_nether":
		return world.Nether, true
	case "minecraft:the_end", "the_end":
		return world.End, true
	}
	return nil, false
}

// GlobalPosCodec is GlobalPos.CODEC: {dimension: "minecraft:overworld", pos: [I; x, y, z]}.
var GlobalPosCodec = Codec[GlobalPos]{
	Encode: func(g GlobalPos) any {
		return map[string]any{"dimension": dimName(g.Dim), "pos": []int32{int32(g.Pos[0]), int32(g.Pos[1]), int32(g.Pos[2])}}
	},
	Decode: func(v any) (GlobalPos, bool) {
		c, _ := v.(map[string]any)
		name, _ := c["dimension"].(string)
		d, ok := dimByName(name)
		if !ok {
			return GlobalPos{}, false
		}
		p, ok := intTriple(c["pos"])
		if !ok {
			return GlobalPos{}, false
		}
		return GlobalPos{Dim: d, Pos: p}, true
	},
}

func intTriple(v any) (cube.Pos, bool) {
	switch t := v.(type) {
	case []int32:
		if len(t) == 3 {
			return cube.Pos{int(t[0]), int(t[1]), int(t[2])}, true
		}
	case []any:
		if len(t) == 3 {
			var p cube.Pos
			for i, x := range t {
				n, ok := x.(int32)
				if !ok {
					return cube.Pos{}, false
				}
				p[i] = int(n)
			}
			return p, true
		}
	}
	return cube.Pos{}, false
}

// UUIDCodec is UUIDUtil.CODEC: an int array of 4.
var UUIDCodec = Codec[uuid.UUID]{
	Encode: func(u uuid.UUID) any {
		out := make([]int32, 4)
		for i := range 4 {
			out[i] = int32(uint32(u[i*4])<<24 | uint32(u[i*4+1])<<16 | uint32(u[i*4+2])<<8 | uint32(u[i*4+3]))
		}
		return out
	},
	Decode: func(v any) (uuid.UUID, bool) {
		a, ok := v.([]int32)
		if !ok || len(a) != 4 {
			return uuid.UUID{}, false
		}
		var u uuid.UUID
		for i, x := range a {
			w := uint32(x)
			u[i*4], u[i*4+1], u[i*4+2], u[i*4+3] = byte(w>>24), byte(w>>16), byte(w>>8), byte(w)
		}
		return u, true
	},
}

// BlockPosCodec is BlockPos.CODEC: an int array of 3.
var BlockPosCodec = Codec[cube.Pos]{
	Encode: func(p cube.Pos) any { return []int32{int32(p[0]), int32(p[1]), int32(p[2])} },
	Decode: intTriple,
}

// PositionTracker is Java's PositionTracker (look and walk targets). tx is
// the transaction of the tick; trackers of removed entities report their
// last known position.
type PositionTracker interface {
	Position(tx *world.Tx) mgl64.Vec3
	BlockPos(tx *world.Tx) cube.Pos
	// VisibleBy is isVisibleBy(body); mem is the body's memories.
	VisibleBy(tx *world.Tx, mem *Memories) bool
}

// BlockPosTracker is Java's BlockPosTracker.
type BlockPosTracker struct {
	Block  cube.Pos
	Center mgl64.Vec3
}

// TrackBlock is new BlockPosTracker(BlockPos): the block's centre.
func TrackBlock(p cube.Pos) BlockPosTracker {
	return BlockPosTracker{Block: p, Center: mgl64.Vec3{float64(p[0]) + .5, float64(p[1]) + .5, float64(p[2]) + .5}}
}

// TrackVec is new BlockPosTracker(Vec3).
func TrackVec(v mgl64.Vec3) BlockPosTracker {
	return BlockPosTracker{Block: Containing(v), Center: v}
}

func (t BlockPosTracker) Position(*world.Tx) mgl64.Vec3       { return t.Center }
func (t BlockPosTracker) BlockPos(*world.Tx) cube.Pos         { return t.Block }
func (t BlockPosTracker) VisibleBy(*world.Tx, *Memories) bool { return true }

// Containing is BlockPos.containing(Vec3): floor of each axis.
func Containing(v mgl64.Vec3) cube.Pos {
	return cube.Pos{int(math.Floor(v[0])), int(math.Floor(v[1])), int(math.Floor(v[2]))}
}

// EntityTracker is Java's EntityTracker.
type EntityTracker struct {
	Handle *world.EntityHandle
	// TrackEye adds the eye height to Position; TargetEye uses the eye
	// position's block for BlockPos.
	TrackEye, TargetEye bool
	last                mgl64.Vec3
}

// TrackEntity is new EntityTracker(entity, trackEyeHeight).
func TrackEntity(h *world.EntityHandle, trackEye bool) *EntityTracker {
	return &EntityTracker{Handle: h, TrackEye: trackEye}
}

func eyeHeight(e world.Entity) float64 {
	if x, ok := e.(interface{ EyeHeight() float64 }); ok {
		return x.EyeHeight()
	}
	return 0
}

func (t *EntityTracker) Position(tx *world.Tx) mgl64.Vec3 {
	if tx != nil {
		if e, ok := t.Handle.Entity(tx); ok {
			t.last = e.Position()
			if t.TrackEye {
				t.last = t.last.Add(mgl64.Vec3{0, eyeHeight(e), 0})
			}
		}
	}
	return t.last
}

func (t *EntityTracker) BlockPos(tx *world.Tx) cube.Pos {
	if tx != nil {
		if e, ok := t.Handle.Entity(tx); ok {
			p := e.Position()
			if t.TargetEye {
				p = p.Add(mgl64.Vec3{0, eyeHeight(e), 0})
			}
			return Containing(p)
		}
	}
	return Containing(t.last)
}

// VisibleBy is EntityTracker.isVisibleBy: a living target must be alive and
// in the body's visible entities; other entities are always visible.
func (t *EntityTracker) VisibleBy(tx *world.Tx, mem *Memories) bool {
	if tx == nil {
		return false
	}
	e, ok := t.Handle.Entity(tx)
	if !ok {
		return false
	}
	if !IsLiving(e) {
		return true
	}
	if !IsAlive(e) {
		return false
	}
	v, ok := Get(mem, MemNearestVisibleLivingEntities)
	return ok && v.Contains(tx, t.Handle)
}

// IsLiving reports whether e is a LivingEntity in Java's sense. The mob
// base may replace it; the default treats anything with Dead() as living.
var IsLiving = func(e world.Entity) bool {
	_, ok := e.(interface{ Dead() bool })
	return ok
}

// IsAlive is Entity.isAlive for living entities (not removed, health > 0).
var IsAlive = func(e world.Entity) bool {
	if d, ok := e.(interface{ Dead() bool }); ok {
		return !d.Dead()
	}
	return true
}

// WalkTarget is Java's WalkTarget.
type WalkTarget struct {
	Target      PositionTracker
	Speed       float32
	CloseEnough int
}

// VisibleEntities is Java's NearestVisibleLivingEntities: the nearby living
// entities sorted by distance and a cached visibility test
// (Sensor.isEntityTargetable) evaluated on demand. The memory outlives the
// tick it was made in, so every query takes the current transaction.
type VisibleEntities struct {
	Nearby []*world.EntityHandle
	test   func(tx *world.Tx, h *world.EntityHandle) bool
	cache  map[*world.EntityHandle]bool
}

// NewVisibleEntities wraps nearby (closest first) with the visibility test.
func NewVisibleEntities(nearby []*world.EntityHandle, test func(tx *world.Tx, h *world.EntityHandle) bool) *VisibleEntities {
	return &VisibleEntities{Nearby: nearby, test: test}
}

func (v *VisibleEntities) visible(tx *world.Tx, h *world.EntityHandle) bool {
	if v.test == nil || tx == nil {
		return false
	}
	if r, ok := v.cache[h]; ok {
		return r
	}
	if v.cache == nil {
		v.cache = make(map[*world.EntityHandle]bool, len(v.Nearby))
	}
	r := v.test(tx, h)
	v.cache[h] = r
	return r
}

// FindClosest is findClosest: the first entity passing filter and visible.
func (v *VisibleEntities) FindClosest(tx *world.Tx, filter func(h *world.EntityHandle) bool) (*world.EntityHandle, bool) {
	if v == nil {
		return nil, false
	}
	for _, h := range v.Nearby {
		if filter(h) && v.visible(tx, h) {
			return h, true
		}
	}
	return nil, false
}

// FindAll is findAll.
func (v *VisibleEntities) FindAll(tx *world.Tx, filter func(h *world.EntityHandle) bool) []*world.EntityHandle {
	if v == nil {
		return nil
	}
	var out []*world.EntityHandle
	for _, h := range v.Nearby {
		if filter(h) && v.visible(tx, h) {
			out = append(out, h)
		}
	}
	return out
}

// Contains is contains(entity).
func (v *VisibleEntities) Contains(tx *world.Tx, h *world.EntityHandle) bool {
	if v == nil {
		return false
	}
	for _, x := range v.Nearby {
		if x == h {
			return v.visible(tx, h)
		}
	}
	return false
}

// ContainsMatch is contains(predicate).
func (v *VisibleEntities) ContainsMatch(tx *world.Tx, filter func(h *world.EntityHandle) bool) bool {
	_, ok := v.FindClosest(tx, filter)
	return ok
}
