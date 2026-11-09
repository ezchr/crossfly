//go:build crossfly_full

// Package sense holds the helpers the hostile goals (mob/goal/combat) and
// components (mob/component/combat) share: line of sight with the per-tick
// cache of BDS SensingComponent, field of view, daylight, difficulty, event
// triggers and value ranges. It registers nothing.
package sense

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Shooter is implemented by the minecraft:shooter component. The ranged
// attack goal finds it with mob.ComponentOf[sense.Shooter].
type Shooter interface {
	Shoot(m *mob.Mob, target world.Entity)
}

// AngerTimer is implemented by the minecraft:angry component: attack goals
// restart it after each hit (BDS AngryComponent::restartTimer).
type AngerTimer interface {
	RestartAngerTimer(m *mob.Mob)
}

// KnockbackApplier is implemented by minecraft:apply_knockback_rules: the
// melee hit (goal/combat DoHurtTarget) lets it knock t back instead of the
// default 0.4/0.4. It reports false when no preset applies.
type KnockbackApplier interface {
	ApplyKnockback(m *mob.Mob, t world.Entity, damage float64) bool
}

// senseKey is the mob.Data key of the per-tick sight cache.
type senseKey struct{}

type senseCache struct {
	tick int64
	seen map[*world.EntityHandle]bool
}

// CanSee reports whether m can see e: a block ray trace from m's eyes to
// e's eyes (BDS Actor::canSee). The answer is cached for the rest of the
// mob's tick, as BDS SensingComponent does with its seen/unseen sets.
func CanSee(m *mob.Mob, e world.Entity) bool {
	if e == nil {
		return false
	}
	c, _ := m.Data(senseKey{})
	cache, _ := c.(*senseCache)
	if cache == nil {
		cache = &senseCache{seen: map[*world.EntityHandle]bool{}}
		m.SetData(senseKey{}, cache)
	}
	if cache.tick != m.Ticks() {
		cache.tick = m.Ticks()
		clear(cache.seen)
	}
	if v, ok := cache.seen[e.H()]; ok {
		return v
	}
	v := LineOfSight(m.Tx(), m.EyePosition(), EyePos(e))
	cache.seen[e.H()] = v
	return v
}

// LineOfSight reports whether no block model lies between from and to.
func LineOfSight(tx *world.Tx, from, to mgl64.Vec3) bool {
	clear := true
	trace.TraverseBlocks(from, to, func(pos cube.Pos) bool {
		b := tx.Block(pos)
		if _, ok := trace.BlockIntercept(pos, tx, b, from, to); ok {
			clear = false
			return false
		}
		return true
	})
	return clear
}

// EyePos returns the eye position of any entity.
func EyePos(e world.Entity) mgl64.Vec3 {
	switch e := e.(type) {
	case *mob.Mob:
		return e.EyePosition()
	case interface {
		world.Entity
		EyeHeight() float64
	}:
		return e.Position().Add(mgl64.Vec3{0, e.EyeHeight()})
	}
	if m, ok := mob.Of(e); ok {
		return m.EyePosition()
	}
	return e.Position().Add(mgl64.Vec3{0, e.H().Type().BBox(e).Height() * 0.85})
}

// BBox returns e's bounding box at its position.
func BBox(e world.Entity) cube.BBox {
	return e.H().Type().BBox(e).Translate(e.Position())
}

// Width is the width of e's box (BDS getAABBDim().x).
func Width(e world.Entity) float64 { return e.H().Type().BBox(e).Width() }

// Center is the middle of e's box (BDS attach position "body").
func Center(e world.Entity) mgl64.Vec3 {
	b := BBox(e)
	return b.Min().Add(b.Max()).Mul(0.5)
}

// HorizontalDistSq is the squared distance between a and b ignoring height.
func HorizontalDistSq(a, b mgl64.Vec3) float64 {
	dx, dz := a[0]-b[0], a[2]-b[2]
	return dx*dx + dz*dz
}

// BoxDistance is the distance between the closest points of two boxes (BDS
// AABB::distanceTo(AABB)).
func BoxDistance(a, b cube.BBox) float64 {
	var sq float64
	for i := 0; i < 3; i++ {
		var d float64
		switch {
		case a.Min()[i] > b.Max()[i]:
			d = a.Min()[i] - b.Max()[i]
		case b.Min()[i] > a.Max()[i]:
			d = b.Min()[i] - a.Max()[i]
		}
		sq += d * d
	}
	return math.Sqrt(sq)
}

// WithinFOV reports whether pos lies within fov degrees of the direction m's
// head faces, seen from m's feet (BDS SensingComponent::withinFOV).
func WithinFOV(m *mob.Mob, pos mgl64.Vec3, fov float64) bool {
	yaw, pitch := mgl64.DegToRad(m.HeadYaw()), mgl64.DegToRad(m.Pitch())
	dir := mgl64.Vec3{-math.Cos(pitch) * math.Sin(yaw), -math.Sin(pitch), math.Cos(pitch) * math.Cos(yaw)}
	d := pos.Sub(m.Pos())
	if d.Len() < 0.0001 {
		d = mgl64.Vec3{}
	} else {
		d = d.Normalize()
	}
	dot := math.Max(-1, math.Min(1, dir.Dot(d)))
	return fov > mgl64.RadToDeg(math.Acos(dot))
}

// YawTowards is the yaw (degrees) that faces from a to b.
func YawTowards(a, b mgl64.Vec3) float64 {
	return mgl64.RadToDeg(math.Atan2(-(b[0] - a[0]), b[2]-a[2]))
}

// Alive reports whether e is in the world and, if living, not dead.
func Alive(e world.Entity) bool {
	if e == nil || e.H().Closed() {
		return false
	}
	if l, ok := e.(entity.Living); ok {
		return !l.Dead()
	}
	return true
}

// Invulnerable reports whether e is a player that cannot take damage
// (creative, spectator: BDS ability "invulnerable").
func Invulnerable(e world.Entity) bool {
	if p, ok := e.(*player.Player); ok {
		return !p.GameMode().AllowsTakingDamage()
	}
	return false
}

// IsPlayer reports whether e is a player.
func IsPlayer(e world.Entity) bool {
	_, ok := e.(*player.Player)
	return ok
}

// Sneaking reports whether e sneaks.
func Sneaking(e world.Entity) bool {
	s, ok := e.(interface{ Sneaking() bool })
	return ok && s.Sneaking()
}

// Invisible reports whether e is invisible.
func Invisible(e world.Entity) bool {
	s, ok := e.(interface{ Invisible() bool })
	return ok && s.Invisible()
}

// ArmourCover is the fraction of armour slots e has filled (BDS
// Mob::getArmorCoverPercentage).
func ArmourCover(e world.Entity) float64 {
	a := ArmourOf(e)
	if a == nil {
		return 0
	}
	var filled float64
	for _, s := range []item.Stack{a.Helmet(), a.Chestplate(), a.Leggings(), a.Boots()} {
		if !s.Empty() {
			filled++
		}
	}
	return filled / 4
}

// ArmourOf returns e's armour slots, or nil.
func ArmourOf(e world.Entity) *inventory.Armour {
	if a, ok := e.(interface{ Armour() *inventory.Armour }); ok {
		return a.Armour()
	}
	return nil
}

func isWater(l world.Liquid) bool {
	_, ok := l.(block.Water)
	return ok
}

// DifficultyID returns the world difficulty: 0 peaceful, 1 easy, 2 normal,
// 3 hard.
func DifficultyID(tx *world.Tx) int {
	if tx == nil {
		return 2
	}
	id, ok := world.DifficultyID(tx.World().Difficulty())
	if !ok {
		return 2
	}
	return id
}

// IsDay reports whether it is day (sun above the horizon, the times at which
// the sky darkening is below 4: 23460..12542).
// source: java/paper Level.isDay; BDS Level::isDay was not in the dump.
func IsDay(tx *world.Tx) bool {
	t := tx.World().Time() % 24000
	if t < 0 {
		t += 24000
	}
	return t < 12542 || t >= 23460
}

// SkyVisible reports whether nothing above pos blocks or dims sky light
// (BDS BlockSource::canSeeSky): pos is above the highest light blocker of its
// column and gets full sky light.
func SkyVisible(tx *world.Tx, pos cube.Pos) bool {
	return pos[1] > tx.HighestLightBlocker(pos[0], pos[2]) && tx.SkyLight(pos) >= 15
}

// Brightness is the 0..1 brightness at pos (BrightnessOf its light level).
func Brightness(tx *world.Tx, pos cube.Pos) float64 {
	return BrightnessOf(tx.Light(pos))
}

// BrightnessOf is the Java/BDS brightness ramp of a light level (ambient
// light 0), in float32 like the game: 12 lands just above 0.5, so daylight
// burning starts at light 12.
func BrightnessOf(light uint8) float64 {
	f := float32(light) / 15
	return float64(f / (4 - 3*f))
}

// InWaterOrRain reports whether e stands in water or rain falls on it.
func InWaterOrRain(tx *world.Tx, e world.Entity) bool {
	pos := cube.PosFromVec3(e.Position())
	if l, ok := tx.Liquid(pos); ok && isWater(l) {
		return true
	}
	if m, ok := e.(*mob.Mob); ok && m.Physics().InWater {
		return true
	}
	return tx.RainingAt(pos) && SkyVisible(tx, pos)
}
