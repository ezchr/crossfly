//go:build crossfly_full

package hostile

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// CustomHitTest is minecraft:custom_hit_test (hoglin, zoglin; BDS
// CustomHitTestComponent): the boxes players hit the mob with instead of
// its collision box: each hitbox is width x height around the mob's
// position plus pivot (the pivot is the box centre). Dragonfly's attack
// ray uses the entity type's BBox, so this is data: HitBoxes returns the
// boxes for code that wants them (debug, a future hit test hook).
type CustomHitTest struct {
	boxes []hitBox
}

type hitBox struct {
	width, height float64
	pivot         mgl64.Vec3
}

func newCustomHitTest(key string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		Hitboxes []struct {
			Width  *float64  `json:"width"`
			Height *float64  `json:"height"`
			Pivot  []float64 `json:"pivot"`
		} `json:"hitboxes"`
	}
	if !kit.IsNull(raw) {
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("%v: %w", key, err)
		}
	}
	c := &CustomHitTest{}
	for _, h := range v.Hitboxes {
		b := hitBox{width: kit.Or(h.Width, 1), height: kit.Or(h.Height, 1)}
		if len(h.Pivot) == 3 {
			b.pivot = mgl64.Vec3{h.Pivot[0], h.Pivot[1], h.Pivot[2]}
		}
		c.boxes = append(c.boxes, b)
	}
	return c, nil
}

// Attach ...
func (*CustomHitTest) Attach(*mob.Mob) {}

// Detach ...
func (*CustomHitTest) Detach(*mob.Mob) {}

// HitBoxes returns the mob's hit boxes in world space.
func (c *CustomHitTest) HitBoxes(m *mob.Mob) []cube.BBox {
	out := make([]cube.BBox, 0, len(c.boxes))
	p := m.Position()
	s := m.Scale()
	for _, b := range c.boxes {
		ctr := p.Add(b.pivot.Mul(s))
		hw, hh := b.width*s/2, b.height*s/2
		out = append(out, cube.Box(ctr[0]-hw, ctr[1]-hh, ctr[2]-hw, ctr[0]+hw, ctr[1]+hh, ctr[2]+hw))
	}
	return out
}

// GroupSize is minecraft:group_size (hoglin, piglin; BDS
// GroupSizeSystem::_tickComponent): every 10 ticks it counts the mob plus
// the mobs of its type within radius (16) for which filters pass (the
// counted mob as "self"). AvoidMobTypeGoal compares group sizes (hoglins
// flee from larger piglin groups); Size exposes the count.
type GroupSize struct {
	radius float64
	filter mob.Filter
	size   int
	next   int64
}

func newGroupSize(key string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		Radius  *float64        `json:"radius"`
		Filters json.RawMessage `json:"filters"`
	}
	if !kit.IsNull(raw) {
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("%v: %w", key, err)
		}
	}
	f, err := kit.Filter(v.Filters)
	if err != nil {
		return nil, fmt.Errorf("%v: filters: %w", key, err)
	}
	return &GroupSize{radius: kit.Or(v.Radius, 16), filter: f, size: 1}, nil
}

// Attach ...
func (*GroupSize) Attach(*mob.Mob) {}

// Detach ...
func (*GroupSize) Detach(*mob.Mob) {}

// Size is the last counted group size (at least 1).
func (c *GroupSize) Size() int { return c.size }

// GroupSizeOf returns m's group size, 1 without group_size.
func GroupSizeOf(m *mob.Mob) int {
	if c, ok := mob.ComponentOf[*GroupSize](m); ok {
		return c.size
	}
	return 1
}

func (c *GroupSize) tick(m *mob.Mob) {
	if m.Ticks() < c.next {
		return
	}
	c.next = m.Ticks() + 10
	n := 1
	r := c.radius
	for _, e := range kit.Around(m, r, r) {
		om, ok := kit.Mob(e)
		if !ok || om.Dead() || om.Identifier() != m.Identifier() {
			continue
		}
		ctx := om.Ctx()
		ctx.Other = m
		if c.filter.Test(ctx) {
			n++
		}
	}
	c.size = n
}

// ReflectProjectiles is minecraft:reflect_projectiles (breeze, 1.26; not
// in the 1.21.3 dump; source: the 1.26 JSON and java/paper Breeze
// deflection): projectiles whose type is in reflected_projectiles that are
// about to hit the mob are turned around: their velocity is rotated by
// azimuth_angle (Molang, degrees around the vertical, 180 = straight back),
// tilted by elevation_angle (Molang, 0) and scaled by reflection_scale
// (Molang, 1); reflection_sound ("reflect") plays.
//
// Dragonfly's projectile closes once it hit an entity unless that entity
// reports a shield block, so the reflection happens one step before the
// hit: every tick the mob turns listed projectiles whose next move would
// enter its box. A listed projectile that still hits does no damage.
type ReflectProjectiles struct {
	types     map[string]bool
	azimuth   core.Expr
	elevation core.Expr
	scale     core.Expr
	sound     string
	done      map[*world.EntityHandle]int64
}

func newReflectProjectiles(key string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		Types     []string        `json:"reflected_projectiles"`
		Azimuth   json.RawMessage `json:"azimuth_angle"`
		Elevation json.RawMessage `json:"elevation_angle"`
		Scale     json.RawMessage `json:"reflection_scale"`
		Sound     *string         `json:"reflection_sound"`
	}
	if !kit.IsNull(raw) {
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("%v: %w", key, err)
		}
	}
	c := &ReflectProjectiles{types: map[string]bool{}, sound: kit.Or(v.Sound, "reflect"), done: map[*world.EntityHandle]int64{}}
	for _, t := range v.Types {
		if !strings.Contains(t, ":") {
			t = "minecraft:" + t
		}
		c.types[t] = true
	}
	var err error
	if c.azimuth, err = exprOr(v.Azimuth, 180); err != nil {
		return nil, fmt.Errorf("%v: azimuth_angle: %w", key, err)
	}
	if c.elevation, err = exprOr(v.Elevation, 0); err != nil {
		return nil, fmt.Errorf("%v: elevation_angle: %w", key, err)
	}
	if c.scale, err = exprOr(v.Scale, 1); err != nil {
		return nil, fmt.Errorf("%v: reflection_scale: %w", key, err)
	}
	return c, nil
}

func exprOr(raw json.RawMessage, def float64) (core.Expr, error) {
	if kit.IsNull(raw) {
		return core.Const(def), nil
	}
	return core.ParseExpr(raw)
}

// Attach ...
func (*ReflectProjectiles) Attach(*mob.Mob) {}

// Detach ...
func (*ReflectProjectiles) Detach(*mob.Mob) {}

// Reflects reports whether projectiles of the entity type id are reflected.
func (c *ReflectProjectiles) Reflects(id string) bool { return c.types[id] }

// Reflected returns the velocity v of a projectile leaves the mob with.
func (c *ReflectProjectiles) Reflected(m *mob.Mob, v mgl64.Vec3) mgl64.Vec3 {
	az := mgl64.DegToRad(c.azimuth.Eval(m))
	x, z := v[0]*math.Cos(az)-v[2]*math.Sin(az), v[0]*math.Sin(az)+v[2]*math.Cos(az)
	h := math.Hypot(x, z)
	if el := mgl64.DegToRad(c.elevation.Eval(m)); el != 0 {
		l := math.Hypot(h, v[1])
		pitch := math.Atan2(v[1], h) + el
		nh := l * math.Cos(pitch)
		if h > 1e-6 {
			x, z = x/h*nh, z/h*nh
		}
		v[1] = l * math.Sin(pitch)
	}
	return mgl64.Vec3{x, v[1], z}.Mul(c.scale.Eval(m))
}

func (c *ReflectProjectiles) tick(m *mob.Mob) {
	if m.Dead() || len(c.types) == 0 {
		return
	}
	now := m.Ticks()
	for h, t := range c.done {
		if now-t > 40 {
			delete(c.done, h)
		}
	}
	box := m.BBox().Grow(0.3)
	for e := range m.Tx().EntitiesWithin(m.BBox().Grow(6)) {
		if !c.types[e.H().Type().EncodeEntity()] {
			continue
		}
		if _, ok := c.done[e.H()]; ok {
			continue
		}
		ve, ok := e.(interface {
			Velocity() mgl64.Vec3
			SetVelocity(mgl64.Vec3)
		})
		if !ok {
			continue
		}
		v := ve.Velocity()
		if v.Len() < 1e-3 {
			continue
		}
		if _, hit := trace.BBoxIntercept(box, e.Position(), e.Position().Add(v.Mul(1.5))); !hit && !box.Vec3Within(e.Position()) {
			continue
		}
		ve.SetVelocity(c.Reflected(m, v))
		c.done[e.H()] = now
		m.PlaySound(c.sound)
	}
}

// ModifyDamage cancels damage from a listed projectile that got through.
func (c *ReflectProjectiles) ModifyDamage(_ *mob.Mob, d *mob.Damage) {
	if s, ok := d.Source.(entity.ProjectileDamageSource); ok && s.Projectile != nil && c.types[s.Projectile.H().Type().EncodeEntity()] {
		d.Cancel = true
	}
}
