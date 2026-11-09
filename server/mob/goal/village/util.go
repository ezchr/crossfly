//go:build crossfly_full

package village

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	vc "github.com/df-mc/dragonfly/server/mob/component/village"
	"github.com/df-mc/dragonfly/server/mob/goal/core"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// maxHeadXRot is Mob::getMaxHeadXRot.
const maxHeadXRot = 40

var navigationKeys = []string{
	"minecraft:navigation.walk", "minecraft:navigation.climb", "minecraft:navigation.float",
	"minecraft:navigation.fly", "minecraft:navigation.hover", "minecraft:navigation.swim",
	"minecraft:navigation.generic",
}

func hasNav(m *mob.Mob) bool {
	for _, k := range navigationKeys {
		if m.HasComponent(k) {
			return true
		}
	}
	return false
}

func decode(spec mob.GoalSpec, v any) error {
	if err := spec.Decode(v); err != nil {
		return fmt.Errorf("%v: %w", spec.Key, err)
	}
	return nil
}

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

func distSq(a, b mgl64.Vec3) float64 { return a.Sub(b).LenSqr() }

func hdistSq(a, b mgl64.Vec3) float64 {
	dx, dz := a[0]-b[0], a[2]-b[2]
	return dx*dx + dz*dz
}

// nearby returns entities (not m) within the box around m grown by xz/y,
// nearest first.
func nearby(m *mob.Mob, xz, y float64) []world.Entity {
	box := m.BBox().Extend(mgl64.Vec3{xz, y, xz}).Extend(mgl64.Vec3{-xz, -y, -xz})
	pos := m.Position()
	var out []world.Entity
	for e := range m.Tx().EntitiesWithin(box) {
		if e.H() != m.H() {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b world.Entity) int {
		da, db := distSq(a.Position(), pos), distSq(b.Position(), pos)
		switch {
		case da < db:
			return -1
		case da > db:
			return 1
		}
		return 0
	})
	return out
}

func eyesOf(e world.Entity) mgl64.Vec3 {
	if m, ok := mob.Of(e); ok {
		return m.EyePosition()
	}
	if p, ok := e.(*player.Player); ok {
		return entity.EyePosition(p)
	}
	return e.Position().Add(mgl64.Vec3{0, e.H().Type().BBox(e).Height() * 0.85})
}

func lookAt(m *mob.Mob, e world.Entity, yaw, pitch float64) {
	m.LookControl().SetLookAtEntity(e, yaw, pitch)
}

func lookAtPos(m *mob.Mob, p mgl64.Vec3, yaw, pitch float64) {
	m.LookControl().SetLookAt(p, yaw, pitch)
}

// posTowards is RandomPos::getPosTowards(mob, xz, y, target).
func posTowards(m *mob.Mob, xz, y int, target mgl64.Vec3) (mgl64.Vec3, bool) {
	dir := target.Sub(m.Position())
	return core.RandomPos(m, xz, y, 0, &dir, 10)
}

// randomPos is RandomPos::getPos.
func randomPos(m *mob.Mob, xz, y int) (mgl64.Vec3, bool) {
	return core.RandomPos(m, xz, y, 0, nil, 10)
}

func centre(p mgl64.Vec3) mgl64.Vec3 { return mgl64.Vec3{p[0] + 0.5, p[1], p[2] + 0.5} }

// topSolid is getAboveTopSolidBlock at x, z.
func topSolid(tx *world.Tx, x, y, z float64) mgl64.Vec3 {
	bx, bz := int(math.Floor(x)), int(math.Floor(z))
	return mgl64.Vec3{float64(bx) + 0.5, float64(vc.TopSolid(tx, bx, int(math.Floor(y)), bz)), float64(bz) + 0.5}
}

// rangeF is a number, [a, b], {min, max} or {range_min, range_max}.
type rangeF = vc.FloatRange

func randIn(m *mob.Mob, r rangeF) float64 {
	if r.Max <= r.Min {
		return r.Min
	}
	return r.Min + m.Rand().Float64()*(r.Max-r.Min)
}

// filterList is a list of {"filters": ...} entries (entity_types &co).
type filterList []mob.Filter

func (l *filterList) UnmarshalJSON(b []byte) error {
	var arr []json.RawMessage
	if err := json.Unmarshal(b, &arr); err != nil {
		arr = []json.RawMessage{b}
	}
	for _, r := range arr {
		var o struct {
			Filters json.RawMessage `json:"filters"`
		}
		if json.Unmarshal(r, &o) != nil || o.Filters == nil {
			continue
		}
		f, err := mob.CompileFilter(o.Filters)
		if err != nil {
			return err
		}
		*l = append(*l, f)
	}
	return nil
}

func (l filterList) any(m *mob.Mob, other world.Entity) bool {
	if len(l) == 0 {
		return true
	}
	ctx := m.Ctx()
	ctx.Other = other
	for _, f := range l {
		if f.Test(ctx) {
			return true
		}
	}
	return false
}

func compile(raw json.RawMessage) mob.Filter {
	if len(raw) == 0 {
		return nil
	}
	f, err := mob.CompileFilter(raw)
	if err != nil {
		return nil
	}
	return f
}

func pass(f mob.Filter, m *mob.Mob) bool { return f == nil || f.Test(m.Ctx()) }

func baby(m *mob.Mob) bool { return m.HasComponent("minecraft:is_baby") }

func blockPos(v mgl64.Vec3) cube.Pos { return cube.PosFromVec3(v) }

// canSeeSky is BlockSource::canSeeSky at pos.
func canSeeSky(tx *world.Tx, pos cube.Pos) bool {
	return tx.HighestLightBlocker(pos[0], pos[2]) < pos[1]
}

func raining(tx *world.Tx, pos cube.Pos) bool { return tx.RainingAt(pos) }

func isDay(tx *world.Tx) bool {
	t := tx.World().Time() % 24000
	return t < 12000 || t >= 23000
}

// moveToPOI is BDS MoveToPOIGoal: walk to the mob's owned POI of a type
// (the closest one of the village if it owns none and fallback is set).
type moveToPOI struct {
	typ      vc.POIType
	speed    float64
	poi      *vc.POI
	target   mgl64.Vec3
	reached  bool
	repath   int
	fallback bool
}

func (g *moveToPOI) find(m *mob.Mob) bool {
	v, ok := vc.VillageOf(m)
	if !ok {
		return false
	}
	p, ok := v.OwnedPOI(m.H(), g.typ)
	if !ok && g.fallback {
		p, ok = v.ClosestPOI(g.typ, m.Position())
	}
	if !ok || !p.Pathable() {
		return false
	}
	g.poi, g.target = p, p.Pos.Vec3Centre()
	g.target[1] = float64(p.Pos[1])
	return true
}

func (g *moveToPOI) start(m *mob.Mob) {
	g.reached, g.repath = false, 0
	g.move(m)
}

func (g *moveToPOI) move(m *mob.Mob) {
	if !m.Navigation().MoveTo(m, g.target, g.speed) && g.poi != nil {
		g.poi.ArrivalFailed()
	}
}

// tick walks on and reports whether the POI is reached (within radius
// horizontally, 2 blocks vertically).
func (g *moveToPOI) tick(m *mob.Mob, radius float64) bool {
	if g.reached {
		return true
	}
	pos := m.Position()
	if hdistSq(pos, g.target) <= radius*radius && math.Abs(pos[1]-g.target[1]) < 2 {
		g.reached = true
		if g.poi != nil {
			g.poi.Arrived()
		}
		m.Navigation().Stop(m)
		return true
	}
	if g.repath++; m.Navigation().Done() && g.repath > 20 {
		g.repath = 0
		g.move(m)
	}
	return false
}

func (g *moveToPOI) valid() bool { return g.poi.Valid() }

func (g *moveToPOI) stop(m *mob.Mob) {
	m.Navigation().Stop(m)
	g.poi, g.reached = nil, false
}
