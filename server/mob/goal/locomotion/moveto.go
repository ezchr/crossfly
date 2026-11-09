//go:build crossfly_full

package locomotion

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/go-gl/mathgl/mgl64"
)

// moveToKind is the BaseMoveToBlockGoal subclass.
type moveToKind uint8

const (
	moveToLiquid moveToKind = iota // move_to_water, move_to_liquid
	moveToLand
)

// liquidMaterial is move_to_liquid's material_type.
type liquidMaterial uint8

const (
	materialAny liquidMaterial = iota
	materialWater
	materialLava
)

// maxReachChecks bounds the path searches of one cylinder scan (BDS tries
// every valid block in distance order until one is reachable).
const maxReachChecks = 16

// moveToBlock is BaseMoveToGoal/BaseMoveToBlockGoal with the MoveToLiquidGoal
// (minecraft:behavior.move_to_water, move_to_liquid) and MoveToLandGoal
// (minecraft:behavior.move_to_land) target tests.
//
//	speed_multiplier 1, search_range 0, search_height 1, search_count 10,
//	goal_radius 0.5, material_type "Any" (move_to_liquid). Flags move|look.
//
// It searches every 200-399 ticks (the first time at once). Liquid goals take
// the nearest block of a cylinder (search_range x search_height) whose top has
// the liquid with air above it; land goals, used in water, take the best of
// search_count random spots (or scan shells outward when search_count <= 0)
// that are solid with air above. The goal ends on arrival (in the liquid / out
// of the water), after 1200 ticks of travel or stay, or when the target stops
// being valid; a path that cannot start makes it wait 8 s.
type moveToBlock struct {
	mob.GoalBase
	kind     moveToKind
	material liquidMaterial

	speed                     float64
	searchRange, searchHeight int
	searchCount               int
	goalRadiusSq              float64
	nextStart, cooldown       int
	travelTicks, stayTicks    int
	target                    cube.Pos
	hasTarget, reached        bool
}

const (
	moveToMaxTravel = 1200
	moveToMaxStay   = 1200
	moveToCooldown  = 160
	moveToInterval  = 200
)

func newMoveTo(kind moveToKind, defMaterial liquidMaterial) mob.GoalFactory {
	return func(spec mob.GoalSpec) (mob.Goal, error) {
		j := struct {
			Speed        *float64 `json:"speed_multiplier"`
			SearchRange  *int     `json:"search_range"`
			SearchHeight *int     `json:"search_height"`
			SearchCount  *int     `json:"search_count"`
			GoalRadius   *float64 `json:"goal_radius"`
			Material     string   `json:"material_type"`
		}{}
		if err := decodeGoal(spec, &j); err != nil {
			return nil, err
		}
		g := &moveToBlock{
			GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)},
			kind:     kind, material: defMaterial, speed: 1, searchHeight: 1, searchCount: 10, goalRadiusSq: 0.25,
		}
		if j.Speed != nil {
			g.speed = *j.Speed
		}
		if j.SearchRange != nil {
			g.searchRange = *j.SearchRange
		}
		if j.SearchHeight != nil {
			g.searchHeight = *j.SearchHeight
		}
		if j.SearchCount != nil {
			g.searchCount = *j.SearchCount
		}
		if j.GoalRadius != nil {
			g.goalRadiusSq = *j.GoalRadius * *j.GoalRadius
		}
		switch strings.ToLower(j.Material) {
		case "water":
			g.material = materialWater
		case "lava":
			g.material = materialLava
		case "any":
			g.material = materialAny
		}
		return g, nil
	}
}

// inMaterial reports whether the liquid layer at the mob's feet is the goal's
// material.
func (g *moveToBlock) inMaterial(m *mob.Mob) bool {
	return g.isMaterial(m, cube.PosFromVec3(m.Position()))
}

func (g *moveToBlock) isMaterial(m *mob.Mob, pos cube.Pos) bool {
	tx := m.Tx()
	switch g.material {
	case materialWater:
		return waterAt(tx, pos)
	case materialLava:
		return lavaAt(tx, pos)
	}
	_, ok := liquidAt(tx, pos)
	return ok
}

func (g *moveToBlock) kindAllows(m *mob.Mob) bool {
	if g.kind == moveToLand {
		return m.Physics().InWater
	}
	return !g.inMaterial(m)
}

func (g *moveToBlock) CanUse(m *mob.Mob) bool {
	if !g.kindAllows(m) {
		return false
	}
	if g.cooldown > 0 {
		g.cooldown--
		return false
	}
	if !hasNavigation(m) {
		return false
	}
	if g.nextStart > 0 {
		g.nextStart--
		return false
	}
	g.nextStart = moveToInterval + m.Rand().IntN(moveToInterval)
	return g.findTarget(m)
}

func (g *moveToBlock) CanContinueToUse(m *mob.Mob) bool {
	if !g.kindAllows(m) || g.cooldown != 0 || g.stayTicks > moveToMaxStay || g.travelTicks > moveToMaxTravel {
		return false
	}
	if !g.hasTarget || !g.valid(m, g.target) {
		return false
	}
	if p := m.Navigation().Path(); p != nil && p.Len() > 0 {
		end := p.End()
		goal := g.target.Side(cube.FaceUp)
		if d := vec(end).Sub(vec(goal)).LenSqr(); d > math.Max(g.goalRadiusSq, 1) && !p.Done() {
			// The path leads somewhere else (BDS: path end vs target +
			// offset within goal_radius).
			return false
		}
	}
	return true
}

func (g *moveToBlock) Start(m *mob.Mob) {
	g.moveToTarget(m)
	g.travelTicks, g.stayTicks = 0, 0
}

func (g *moveToBlock) Tick(m *mob.Mob) {
	t := blockCenter(g.target.Side(cube.FaceUp))
	if t.Sub(m.Position()).LenSqr() <= g.goalRadiusSq {
		g.stayTicks++
		g.reached = true
		return
	}
	g.travelTicks++
	g.reached = false
	if m.Navigation().Done() || g.travelTicks%100 == 0 {
		if !g.moveToTarget(m) {
			g.cooldown = moveToCooldown
		}
	}
}

func (g *moveToBlock) Stop(m *mob.Mob) {
	m.Navigation().Stop(m)
	g.travelTicks = 0
	g.hasTarget = false
}

// moveToTarget is _moveToBlock: the block above the target, for land goals
// two blocks past it (MoveToLandGoal::_moveToBlock).
func (g *moveToBlock) moveToTarget(m *mob.Mob) bool {
	dest := blockCenter(g.target.Side(cube.FaceUp))
	if g.kind == moveToLand {
		dest = dest.Add(normalize(dest.Sub(m.Position())).Mul(2))
	}
	return m.Navigation().MoveTo(m, dest, g.speed)
}

// valid is isValidTarget.
func (g *moveToBlock) valid(m *mob.Mob, pos cube.Pos) bool {
	tx := m.Tx()
	if g.kind == moveToLand {
		return blocksMotion(tx, pos) && airAt(tx, pos.Side(cube.FaceUp))
	}
	w := int(math.Floor(m.BBox().Width()))
	for dx := 0; dx <= w; dx++ {
		for dz := 0; dz <= w; dz++ {
			p := pos.Add(cube.Pos{dx, 1, dz})
			if !g.isMaterial(m, p) || !airAt(tx, p.Side(cube.FaceUp)) {
				return false
			}
		}
	}
	return true
}

// canReach is _canReach: a path to the block above pos ends within
// goal_radius of it.
func (g *moveToBlock) canReach(m *mob.Mob, pos cube.Pos) bool {
	goal := pos.Side(cube.FaceUp)
	p := m.Navigation().CreatePath(m, blockCenter(goal))
	if p == nil || p.Len() == 0 {
		return false
	}
	return vec(p.End()).Sub(vec(goal)).LenSqr() <= math.Max(g.goalRadiusSq, 1)
}

func (g *moveToBlock) findTarget(m *mob.Mob) bool {
	g.hasTarget = false
	c := cube.PosFromVec3(m.Position())
	if g.kind != moveToLand {
		return g.findInCylinder(m, c)
	}
	if g.searchCount <= 0 {
		return g.findInShells(m, c)
	}
	best := 99999.99
	for i := 0; i < g.searchCount; i++ {
		pos, ok := randomPos(m, posQuery{xz: max(g.searchRange, 1), y: max(g.searchHeight, 1), tries: 1})
		if !ok {
			continue
		}
		pos[1]--
		d := float64(distSqInt(pos, c))
		if d < best && g.valid(m, pos) && g.canReach(m, pos) {
			g.target, g.hasTarget, best = pos, true, d
		}
	}
	return g.hasTarget
}

// findInShells is the search_count <= 0 scan (Java MoveToBlockGoal order).
func (g *moveToBlock) findInShells(m *mob.Mob, c cube.Pos) bool {
	best := 99999.99
	for k := 0; k <= g.searchHeight; k = nextShell(k) {
		for r := 0; r < g.searchRange; r++ {
			for dx := 0; dx <= r; dx = nextShell(dx) {
				dz := 0
				if dx < r && dx > -r {
					dz = r
				}
				for ; dz <= r; dz = nextShell(dz) {
					pos := c.Add(cube.Pos{dx, k - 1, dz})
					d := float64(distSqInt(pos, c))
					if d < best && g.valid(m, pos) && g.canReach(m, pos) {
						g.target, g.hasTarget = pos, true
						if d <= g.goalRadiusSq {
							return true
						}
						best = d
					}
				}
			}
		}
	}
	return g.hasTarget
}

// nextShell steps 0, 1, -1, 2, -2, ...
func nextShell(v int) int {
	if v > 0 {
		return -v
	}
	return 1 - v
}

// findInCylinder is MoveToLiquidGoal::findTargetBlock: the blocks of the
// cylinder around c sorted by distance; the first valid, reachable one.
func (g *moveToBlock) findInCylinder(m *mob.Mob, c cube.Pos) bool {
	r := g.searchRange
	var cand []cube.Pos
	for dy := -g.searchHeight; dy <= g.searchHeight; dy++ {
		for dx := -r; dx <= r; dx++ {
			for dz := -r; dz <= r; dz++ {
				if dx*dx+dz*dz > r*r {
					continue
				}
				p := c.Add(cube.Pos{dx, dy, dz})
				if g.valid(m, p) {
					cand = append(cand, p)
				}
			}
		}
	}
	slices.SortStableFunc(cand, func(a, b cube.Pos) int { return distSqInt(a, c) - distSqInt(b, c) })
	for i, p := range cand {
		if i >= maxReachChecks {
			break
		}
		if g.canReach(m, p) {
			g.target, g.hasTarget = p, true
			return true
		}
	}
	return false
}

func distSqInt(a, b cube.Pos) int {
	x, y, z := a[0]-b[0], a[1]-b[1], a[2]-b[2]
	return x*x + y*y + z*z
}

func (g *moveToBlock) AppendDebugInfo(b *strings.Builder) {
	name := "MoveToLiquid"
	if g.kind == moveToLand {
		name = "MoveToLand"
	}
	fmt.Fprintf(b, "%s target %v travel %d stay %d", name, g.target, g.travelTicks, g.stayTicks)
}

var _ = mgl64.Vec3{}
