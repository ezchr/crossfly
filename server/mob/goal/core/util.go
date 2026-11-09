//go:build crossfly_full

package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// maxHeadXRot is Mob::getMaxHeadXRot (the pitch speed goals pass to the look
// control).
const maxHeadXRot = 40

var navigationKeys = []string{
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

// isPlayer reports whether e is a player that can be seen (not a
// spectator).
func isPlayer(e world.Entity) (*player.Player, bool) {
	p, ok := e.(*player.Player)
	if !ok || !p.GameMode().Visible() {
		return nil, false
	}
	return p, true
}

// distSq is the squared distance between two positions.
func distSq(a, b mgl64.Vec3) float64 { return a.Sub(b).LenSqr() }

// nearby returns the entities (other than m) whose position is within the
// box around m grown by xz and y, sorted by distance (BDS
// Actor::fetchNearbyActorsSorted / BlockSource::fetchEntities).
func nearby(m *mob.Mob, xz, y float64) []world.Entity {
	box := m.BBox().Grow(0).Extend(mgl64.Vec3{xz, y, xz}).Extend(mgl64.Vec3{-xz, -y, -xz})
	pos := m.Position()
	var out []world.Entity
	for e := range m.Tx().EntitiesWithin(box) {
		if e.H() == m.H() {
			continue
		}
		out = append(out, e)
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

// eyesOf returns the eye position of e.
func eyesOf(e world.Entity) mgl64.Vec3 {
	if m, ok := mob.Of(e); ok {
		return m.EyePosition()
	}
	if p, ok := e.(*player.Player); ok {
		return entity.EyePosition(p)
	}
	return e.Position().Add(mgl64.Vec3{0, e.H().Type().BBox(e).Height() * 0.85})
}

// widthOf returns the collision width of e.
func widthOf(e world.Entity) float64 { return e.H().Type().BBox(e).Width() }

// timeRange is look_time: [min, max], {"min", "max"} or a number, in
// seconds.
type timeRange struct{ Min, Max float64 }

func (r *timeRange) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	var f float64
	if json.Unmarshal(b, &f) == nil {
		r.Min, r.Max = f, f
		return nil
	}
	var arr []float64
	if json.Unmarshal(b, &arr) == nil {
		if len(arr) > 0 {
			r.Min, r.Max = arr[0], arr[len(arr)-1]
		}
		return nil
	}
	var obj struct {
		Min, Max float64
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return fmt.Errorf("range %s: %w", b, err)
	}
	r.Min, r.Max = obj.Min, obj.Max
	return nil
}

// randInRange returns min + rand(max-min) for ints, as BDS IntRange-like
// fields do (min when max <= min).
func randInRange(m *mob.Mob, lo, hi int) int {
	if hi <= lo {
		return lo
	}
	return lo + m.Rand().IntN(hi-lo)
}

// wrapDegrees wraps an angle to [-180, 180).
func wrapDegrees(a float64) float64 {
	a = math.Mod(a+180, 360)
	if a < 0 {
		a += 360
	}
	return a - 180
}

// floorPos returns the block position of v.
func floorPos(v mgl64.Vec3) cube.Pos { return cube.PosFromVec3(v) }

// soundInterval is a sound_interval {"range_min", "range_max"} in seconds
// (BDS RandomValueBounds), with the next tick a sound may play.
type soundInterval struct {
	Min, Max float64
	next     int64
}

func (s *soundInterval) UnmarshalJSON(b []byte) error {
	var obj struct {
		RangeMin *float64 `json:"range_min"`
		RangeMax *float64 `json:"range_max"`
	}
	var f float64
	if json.Unmarshal(b, &f) == nil {
		s.Min, s.Max = f, f
		return nil
	}
	var arr []float64
	if json.Unmarshal(b, &arr) == nil {
		if len(arr) > 0 {
			s.Min, s.Max = arr[0], arr[len(arr)-1]
		}
		return nil
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	if obj.RangeMin != nil {
		s.Min = *obj.RangeMin
	}
	if obj.RangeMax != nil {
		s.Max = *obj.RangeMax
	}
	return nil
}

// due reports whether the sound should play now and schedules the next.
func (s *soundInterval) due(m *mob.Mob) bool {
	if m.Ticks() <= s.next {
		return false
	}
	d := s.Min
	if s.Max > s.Min {
		d += m.Rand().Float64() * (s.Max - s.Min)
	}
	s.next = m.Ticks() + int64(d*20)
	return true
}
