//go:build crossfly_full

package javamob

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/go-gl/mathgl/mgl64"
)

// WalkTargetValuer is PathfinderMob.getWalkTargetValue: how much the mob likes to walk to pos (random strolls pick
// the best of 10 positions). Monsters prefer the dark (MonsterWalkTargetValue); without it every position is 0.
type WalkTargetValuer interface {
	WalkTargetValue(m *Mob, pos cube.Pos) float64
}

// WalkTargetValue is getWalkTargetValue.
func (m *Mob) WalkTargetValue(pos cube.Pos) float64 {
	if w, ok := m.s.b.(WalkTargetValuer); ok {
		return w.WalkTargetValue(m, pos)
	}
	return 0
}

// MonsterWalkTargetValue is Monster.getWalkTargetValue: -getPathfindingCostFromLightLevels, so 0.5 minus the
// brightness: darker is better.
func MonsterWalkTargetValue(m *Mob, pos cube.Pos) float64 {
	return -(float64(brightness(RawBrightness(m.Tx(), pos))) - 0.5)
}

// SetHome is Mob.setHomeTo: the mob keeps within radius of pos (radius -1: no home).
func (m *Mob) SetHomeTo(pos cube.Pos, radius int) {
	m.SetHome(pos)
	m.s.homeRadius = radius
}

// HomeRadius is getHomeRadius (-1 without a home).
func (m *Mob) HomeRadius() int {
	if _, ok := m.Home(); !ok {
		return -1
	}
	return m.s.homeRadius
}

// WithinHome is Mob.isWithinHome(pos).
func (m *Mob) WithinHome(pos cube.Pos) bool {
	h, ok := m.Home()
	if !ok || m.s.homeRadius < 0 {
		return true
	}
	d := pos.Vec3().Sub(h.Vec3())
	return d.LenSqr() < float64(m.s.homeRadius*m.s.homeRadius)
}

func (m *Mob) restricted(dist float64) bool {
	h, ok := m.Home()
	if !ok || m.s.homeRadius < 0 {
		return false
	}
	r := float64(m.s.homeRadius) + dist + 1
	return h.Vec3Centre().Sub(m.Position()).LenSqr() < r*r
}

func randomDirection(m *Mob, h, v int) cube.Pos {
	r := m.Rand()
	x := r.IntN(2*h+1) - h
	y := r.IntN(2*v+1) - v
	z := r.IntN(2*h+1) - h
	return cube.Pos{x, y, z}
}

func randomDirectionWithinRadians(m *Mob, minH, maxH float64, v, flying int, xDir, zDir, maxRad float64) (cube.Pos, bool) {
	r := m.Rand()
	centre := math.Atan2(zDir, xDir) - float64(float32(math.Pi/2))
	yRad := centre + float64(2*r.Float32()-1)*maxRad
	dist := (minH + math.Sqrt(r.Float64())*(maxH-minH)) * math.Sqrt2
	xt, zt := -dist*math.Sin(yRad), dist*math.Cos(yRad)
	if math.Abs(xt) > maxH || math.Abs(zt) > maxH {
		return cube.Pos{}, false
	}
	yt := r.IntN(2*v+1) - v + flying
	return cube.PosFromVec3(mgl64.Vec3{xt, float64(yt), zt}), true
}

func (m *Mob) randomPosTowardDirection(dist float64, dir cube.Pos) cube.Pos {
	xt, zt := float64(dir[0]), float64(dir[2])
	if h, ok := m.Home(); ok && m.s.homeRadius >= 0 && dist > 1 {
		p := m.Position()
		r := m.Rand()
		if p[0] > float64(h[0]) {
			xt -= r.Float64() * dist / 2
		} else {
			xt += r.Float64() * dist / 2
		}
		if p[2] > float64(h[2]) {
			zt -= r.Float64() * dist / 2
		} else {
			zt += r.Float64() * dist / 2
		}
	}
	p := m.Position()
	return cube.PosFromVec3(mgl64.Vec3{xt + p[0], float64(dir[1]) + p[1], zt + p[2]})
}

func (m *Mob) hasMalus(pos cube.Pos) bool {
	return m.PathfindingMalus(m.s.nav.WalkTypeAt(m, pos)) != 0
}

func (m *Mob) acceptable(pos cube.Pos, restrict bool) bool {
	tx := m.Tx()
	if pos.OutOfBounds(tx.Range()) {
		return false
	}
	if restrict && !m.WithinHome(pos) {
		return false
	}
	return m.s.nav.StableDestination(tx, pos) // GoalUtils.isNotStable: the navigation's isStableDestination
}

// generateRandomPos is RandomPos.generateRandomPos: the best of 10 candidates by walk target value.
func (m *Mob) generateRandomPos(gen func() (cube.Pos, bool)) (mgl64.Vec3, bool) {
	best := math.Inf(-1)
	var bestPos cube.Pos
	found := false
	for i := 0; i < 10; i++ {
		p, ok := gen()
		if !ok {
			continue
		}
		if v := m.WalkTargetValue(p); v > best {
			best, bestPos, found = v, p, true
		}
	}
	if !found {
		return mgl64.Vec3{}, false
	}
	return mgl64.Vec3{float64(bestPos[0]) + 0.5, float64(bestPos[1]), float64(bestPos[2]) + 0.5}, true
}

// DefaultRandomPos is DefaultRandomPos.getPos: a random stable position without malus within h blocks sideways
// and v up or down.
func (m *Mob) DefaultRandomPos(h, v int) (mgl64.Vec3, bool) {
	restrict := m.restricted(float64(h))
	return m.generateRandomPos(func() (cube.Pos, bool) {
		p := m.randomPosTowardDirection(float64(h), randomDirection(m, h, v))
		if !m.acceptable(p, restrict) || m.hasMalus(p) {
			return cube.Pos{}, false
		}
		return p, true
	})
}

// DefaultRandomPosAway is DefaultRandomPos.getPosAway: a random position away from avoid.
func (m *Mob) DefaultRandomPosAway(h, v int, avoid mgl64.Vec3) (mgl64.Vec3, bool) {
	away := m.Position().Sub(avoid)
	restrict := m.restricted(float64(h))
	return m.generateRandomPos(func() (cube.Pos, bool) {
		dir, ok := randomDirectionWithinRadians(m, 0, float64(h), v, 0, away[0], away[2], float64(float32(math.Pi/2)))
		if !ok {
			return cube.Pos{}, false
		}
		p := m.randomPosTowardDirection(float64(h), dir)
		if !m.acceptable(p, restrict) || m.hasMalus(p) {
			return cube.Pos{}, false
		}
		return p, true
	})
}

// LandRandomPos is LandRandomPos.getPos: like DefaultRandomPos, moved up out of solid blocks, never in water.
func (m *Mob) LandRandomPos(h, v int) (mgl64.Vec3, bool) {
	restrict := m.restricted(float64(h))
	return m.generateRandomPos(func() (cube.Pos, bool) {
		p := m.randomPosTowardDirection(float64(h), randomDirection(m, h, v))
		if !m.acceptable(p, restrict) {
			return cube.Pos{}, false
		}
		return m.moveUpOutOfSolid(p)
	})
}

// LandRandomPosAway is LandRandomPos.getPosAway.
func (m *Mob) LandRandomPosAway(h, v int, avoid mgl64.Vec3) (mgl64.Vec3, bool) {
	away := m.Position().Sub(avoid)
	if away.Len() == 0 {
		away = mgl64.Vec3{m.Rand().Float64() - 0.5, 0, m.Rand().Float64() - 0.5}
	}
	return m.landRandomPosInDirection(h, v, away)
}

// LandRandomPosTowards is LandRandomPos.getPosTowards.
func (m *Mob) LandRandomPosTowards(h, v int, towards mgl64.Vec3) (mgl64.Vec3, bool) {
	return m.landRandomPosInDirection(h, v, towards.Sub(m.Position()))
}

func (m *Mob) landRandomPosInDirection(h, v int, dir mgl64.Vec3) (mgl64.Vec3, bool) {
	restrict := m.restricted(float64(h))
	return m.generateRandomPos(func() (cube.Pos, bool) {
		d, ok := randomDirectionWithinRadians(m, 0, float64(h), v, 0, dir[0], dir[2], float64(float32(math.Pi/2)))
		if !ok {
			return cube.Pos{}, false
		}
		p := m.randomPosTowardDirection(float64(h), d)
		if !m.acceptable(p, restrict) {
			return cube.Pos{}, false
		}
		return m.moveUpOutOfSolid(p)
	})
}

// moveUpOutOfSolid is LandRandomPos.movePosUpOutOfSolid.
func (m *Mob) moveUpOutOfSolid(p cube.Pos) (cube.Pos, bool) {
	tx := m.Tx()
	maxY := tx.Range()[1]
	for p[1] <= maxY && path.IsSolid(tx, p) {
		p = p.Side(cube.FaceUp)
	}
	if l, ok := tx.Liquid(p); ok && l.LiquidType() == "water" {
		return cube.Pos{}, false
	}
	if m.hasMalus(p) {
		return cube.Pos{}, false
	}
	return p, true
}
