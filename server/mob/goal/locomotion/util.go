//go:build crossfly_full

package locomotion

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

var navigationKeys = [...]string{
	"minecraft:navigation.walk", "minecraft:navigation.climb", "minecraft:navigation.float",
	"minecraft:navigation.fly", "minecraft:navigation.hover", "minecraft:navigation.swim",
	"minecraft:navigation.generic",
}

// hasNavigation reports whether m has a navigation component (BDS
// Mob::hasComponent<NavigationComponent>).
func hasNavigation(m *mob.Mob) bool {
	for _, k := range navigationKeys {
		if m.HasComponent(k) {
			return true
		}
	}
	return false
}

// navDone reports whether m has no navigation or its navigation is done.
func navDone(m *mob.Mob) bool { return !hasNavigation(m) || m.Navigation().Done() }

// canSwim is Actor::canCurrentlySwim: in water and not levitating.
func canSwim(m *mob.Mob) bool {
	if !m.Physics().InWater {
		return false
	}
	_, lev := m.Effect(effect.Levitation)
	return !lev
}

// inLiquid is NavigationUtility::isInLiquid: in water, or lava applies.
func inLiquid(m *mob.Mob) bool { return m.Physics().InWater || m.Physics().InLava }

// resting reports whether m hangs upside down (bat, Actor::isResting).
func resting(m *mob.Mob) bool { return m.ActorFlag(flagResting) }

// liquidAt returns the liquid in either layer at pos (BDS getLiquidBlock).
func liquidAt(tx *world.Tx, pos cube.Pos) (world.Liquid, bool) {
	if !inRange(tx, pos) {
		return nil, false
	}
	return tx.Liquid(pos)
}

// waterAt reports whether pos holds water (material Water, in either layer).
func waterAt(tx *world.Tx, pos cube.Pos) bool {
	l, ok := liquidAt(tx, pos)
	if !ok {
		return false
	}
	_, w := l.(block.Water)
	return w
}

// lavaAt reports whether pos holds lava.
func lavaAt(tx *world.Tx, pos cube.Pos) bool {
	l, ok := liquidAt(tx, pos)
	if !ok {
		return false
	}
	_, w := l.(block.Lava)
	return w
}

// airAt reports whether pos is air (material Air): an air block with no
// liquid.
func airAt(tx *world.Tx, pos cube.Pos) bool {
	if !inRange(tx, pos) {
		return pos[1] > tx.Range()[1]
	}
	if _, ok := tx.Block(pos).(block.Air); !ok {
		return false
	}
	_, liquid := tx.Liquid(pos)
	return !liquid
}

// blocksMotion reports whether the block at pos has a collision box
// (Material::getBlocksMotion for the vanilla blocks; liquids do not).
func blocksMotion(tx *world.Tx, pos cube.Pos) bool {
	if !inRange(tx, pos) {
		return false
	}
	b := tx.Block(pos)
	switch b.(type) {
	case block.Air, world.Liquid:
		return false
	}
	return len(b.Model().BBox(pos, tx)) > 0
}

func inRange(tx *world.Tx, pos cube.Pos) bool {
	r := tx.Range()
	return pos[1] >= r[0] && pos[1] <= r[1]
}

// isOverWater is Actor::isOverWater: in water, or the first non-air block
// below is water.
func isOverWater(m *mob.Mob) bool {
	if m.Physics().InWater {
		return true
	}
	tx := m.Tx()
	p := cube.PosFromVec3(m.Position())
	min := tx.Range()[0]
	for p[1] >= min && airAt(tx, p) {
		p[1]--
	}
	return waterAt(tx, p)
}

// aboveTopSolidBlock is BlockSource::getAboveTopSolidBlock(pos, false,
// false): the y above the highest block of the column that blocks motion
// (liquids and air are skipped).
func aboveTopSolidBlock(tx *world.Tx, x, z int) int {
	y := int(tx.HighestBlock(x, z))
	min := tx.Range()[0]
	for y >= min && !blocksMotion(tx, cube.Pos{x, y, z}) {
		y--
	}
	return y + 1
}

// waterSurface returns the y of the top water block of the column starting
// at from (from itself when it is not water, minus one).
func waterSurface(tx *world.Tx, x, from, z int) int {
	y := from
	max := tx.Range()[1]
	for y <= max && waterAt(tx, cube.Pos{x, y, z}) {
		y++
	}
	return y - 1
}

// clip reports whether the segment from-to hits a block with a collision box
// (BlockSource::clip with CHECK_ALL_PICKABLE_BLOCKS; liquids are not hit).
func clip(tx *world.Tx, from, to mgl64.Vec3) bool {
	return clipWith(tx, from, to, false)
}

// clipWith is clip; with air also set, air cells count as a hit too (the
// IS_AIR_OR_OBSTACLE predicate swim_wander uses to keep rays in water).
func clipWith(tx *world.Tx, from, to mgl64.Vec3, air bool) bool {
	hit := false
	trace.TraverseBlocks(from, to, func(pos cube.Pos) bool {
		if !inRange(tx, pos) {
			return true
		}
		if air && airAt(tx, pos) {
			hit = true
			return false
		}
		b := tx.Block(pos)
		if _, ok := b.(block.Air); ok {
			return true
		}
		if _, ok := trace.BlockIntercept(pos, tx, b, from, to); ok {
			hit = true
			return false
		}
		return true
	})
	return hit
}

// rotateY turns v by deg degrees about the Y axis (glm::rotate around
// UNIT_Y, right handed).
func rotateY(v mgl64.Vec3, deg float64) mgl64.Vec3 {
	s, c := math.Sincos(mgl64.DegToRad(deg))
	return mgl64.Vec3{v[0]*c + v[2]*s, v[1], -v[0]*s + v[2]*c}
}

// viewVector is the direction m faces (Actor::getViewVector).
func viewVector(m *mob.Mob) mgl64.Vec3 {
	yaw, pitch := mgl64.DegToRad(m.Yaw()), mgl64.DegToRad(m.Pitch())
	return mgl64.Vec3{-math.Sin(yaw) * math.Cos(pitch), -math.Sin(pitch), math.Cos(yaw) * math.Cos(pitch)}
}

// normalize returns v with length 1, or zero for a (near) zero vector.
func normalize(v mgl64.Vec3) mgl64.Vec3 {
	l := v.Len()
	if l < 1e-4 {
		return mgl64.Vec3{}
	}
	return v.Mul(1 / l)
}

// clampRotate turns from towards to by at most max degrees.
func clampRotate(from, to, max float64) float64 {
	d := wrapDegrees(to - from)
	d = mgl64.Clamp(d, -max, max)
	return wrapDegrees(from + d)
}

// wrapDegrees wraps an angle to [-180, 180).
func wrapDegrees(a float64) float64 {
	a = math.Mod(a+180, 360)
	if a < 0 {
		a += 360
	}
	return a - 180
}

// yawOf is the yaw (degrees) of a direction (Java/BDS: -atan2(x, z)).
func yawOf(d mgl64.Vec3) float64 { return -mgl64.RadToDeg(math.Atan2(d[0], d[2])) }

// alive reports whether e is a living, not removed entity.
func alive(tx *world.Tx, e world.Entity) bool {
	if e == nil || e.H().Closed() {
		return false
	}
	if _, ok := e.H().Entity(tx); !ok {
		return false
	}
	if l, ok := e.(interface{ Dead() bool }); ok && l.Dead() {
		return false
	}
	return true
}

// entityBox returns the bounding box of e at its position.
func entityBox(e world.Entity) cube.BBox {
	return e.H().Type().BBox(e).Translate(e.Position())
}

// intRange is an IntRange/FloatRange field: [min, max], {"min","max"} or a
// number.
type floatRange struct {
	Min, Max float64
	set      bool
}

func (r *floatRange) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	var f float64
	if json.Unmarshal(b, &f) == nil {
		r.Min, r.Max, r.set = f, f, true
		return nil
	}
	var arr []float64
	if json.Unmarshal(b, &arr) == nil {
		if len(arr) > 0 {
			r.Min, r.Max, r.set = arr[0], arr[len(arr)-1], true
		}
		return nil
	}
	var obj struct {
		Min *float64 `json:"min"`
		Max *float64 `json:"max"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return fmt.Errorf("range %s: %w", b, err)
	}
	if obj.Min != nil {
		r.Min, r.set = *obj.Min, true
	}
	if obj.Max != nil {
		r.Max, r.set = *obj.Max, true
	}
	return nil
}

// roll returns a random value in [Min, Max].
func (r floatRange) roll(m *mob.Mob) float64 {
	if r.Max <= r.Min {
		return r.Min
	}
	return r.Min + m.Rand().Float64()*(r.Max-r.Min)
}

// rollInt returns a random int in [Min, Max] (UniformInt).
func (r floatRange) rollInt(m *mob.Mob) int {
	lo, hi := int(r.Min), int(r.Max)
	if hi <= lo {
		return lo
	}
	return lo + m.Rand().IntN(hi-lo+1)
}

func decodeGoal(spec mob.GoalSpec, v any) error {
	if err := spec.Decode(v); err != nil {
		return fmt.Errorf("%v: %w", spec.Key, err)
	}
	return nil
}

func blockCenter(p cube.Pos) mgl64.Vec3 {
	return mgl64.Vec3{float64(p[0]) + 0.5, float64(p[1]), float64(p[2]) + 0.5}
}

func vec(p cube.Pos) mgl64.Vec3 { return mgl64.Vec3{float64(p[0]), float64(p[1]), float64(p[2])} }
