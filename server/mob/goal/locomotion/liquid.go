//go:build crossfly_full

package locomotion

import (
	"fmt"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// riseToLiquidLevel is minecraft:behavior.rise_to_liquid_level (BDS
// RiseToLiquidLevelGoal, strider): in a liquid, keep the bottom of the box at
// the liquid surface minus 0.75 + liquid_y_offset, rising by rise_delta or
// sinking by sink_delta a tick (all 0 by default) while more than 0.4 off.
// It makes a sitting mob stand up. Flags jump.
type riseToLiquidLevel struct {
	mob.GoalBase
	offset, rise, sink float64
}

func newRiseToLiquidLevel(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Offset float64 `json:"liquid_y_offset"`
		Rise   float64 `json:"rise_delta"`
		Sink   float64 `json:"sink_delta"`
	}{}
	if err := decodeGoal(spec, &j); err != nil {
		return nil, err
	}
	return &riseToLiquidLevel{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagJump)}, offset: j.Offset, rise: j.Rise, sink: j.Sink}, nil
}

func (g *riseToLiquidLevel) CanUse(m *mob.Mob) bool {
	if !inLiquid(m) {
		return false
	}
	if m.Meta().Sitting {
		m.UpdateMeta(func(meta *mob.Meta) { meta.Sitting = false })
	}
	return true
}

func (g *riseToLiquidLevel) Tick(m *mob.Mob) {
	if !inLiquid(m) {
		return
	}
	tx := m.Tx()
	box := m.BBox()
	cx, cz := (box.Min()[0]+box.Max()[0])/2, (box.Min()[2]+box.Max()[2])/2
	last, found := -1.0, false
	for y := box.Min()[1] + 0.5; y < float64(tx.Range()[1]); y += 0.1 {
		if _, ok := liquidAt(tx, cube.PosFromVec3(mgl64.Vec3{cx, y, cz})); !ok {
			break
		}
		last, found = y, true
	}
	if !found {
		return
	}
	surface := (1 - liquidHeight(tx, cube.PosFromVec3(mgl64.Vec3{cx, last, cz}))) + last - 0.75 - g.offset
	diff := box.Min()[1] - surface
	v := m.Vel()
	switch {
	case math.Abs(diff) <= 0.4:
		v[1] = 0
	case surface < box.Min()[1]:
		v[1] -= g.sink
	default:
		v[1] += g.rise
	}
	m.SetVelocity(v)
}

// liquidHeight is LiquidBlock::getHeightFromDepth of the liquid at pos: the
// part of the block above the liquid's top, (depth + 1) / 9 for BDS depth <
// 8 (0 for a source), 1/9 for falling liquid.
func liquidHeight(tx *world.Tx, pos cube.Pos) float64 {
	l, ok := liquidAt(tx, pos)
	if !ok {
		return 0
	}
	depth := 8 - l.LiquidDepth() // Dragonfly: 8 = source; BDS: 0 = source
	if l.LiquidFalling() || depth < 0 || depth >= 8 {
		depth = 8
	}
	if depth >= 8 {
		return 1.0 / 9
	}
	return float64(depth+1) / 9
}

func (g *riseToLiquidLevel) AppendDebugInfo(b *strings.Builder) { b.WriteString("RiseToLiquidLevel") }

// swimUpForBreath is minecraft:behavior.swim_up_for_breath (dolphin): BDS
// 1.21.3 hard-codes it as BreatheAirGoal ("minecraft:breathe_air"). When the
// air supply drops below max(5 x the depth below the nearest air, 150) ticks
// it paths at speed 1.4 to an air block with water under it within 4 blocks
// (searching up to 16 blocks up), or straight up when there is none. Flags
// move|look.
type swimUpForBreath struct {
	mob.GoalBase
	speed     float64
	radius    int
	threshold int
	target    mgl64.Vec3
	found     bool
}

func newSwimUpForBreath(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Speed *float64 `json:"speed_multiplier"`
	}{}
	if err := decodeGoal(spec, &j); err != nil {
		return nil, err
	}
	g := &swimUpForBreath{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, speed: 1.4, radius: 4}
	if j.Speed != nil {
		g.speed = *j.Speed
	}
	return g, nil
}

// airSupply returns the breathable component's air supply in ticks.
func airSupply(m *mob.Mob) (int, bool) {
	c, ok := m.Component("minecraft:breathable")
	if !ok {
		return 0, false
	}
	a, ok := c.(interface{ Air() (int, int) })
	if !ok {
		return 0, false
	}
	air, _ := a.Air()
	return air, true
}

func (g *swimUpForBreath) CanUse(m *mob.Mob) bool {
	if !hasNavigation(m) {
		return false
	}
	air, ok := airSupply(m)
	if !ok {
		return false
	}
	g.threshold = max(5*g.depth(m), 150)
	return air < g.threshold
}

func (g *swimUpForBreath) CanContinueToUse(m *mob.Mob) bool {
	air, ok := airSupply(m)
	return ok && air < g.threshold
}

func (g *swimUpForBreath) Start(m *mob.Mob) {
	g.target = mgl64.Vec3{}
	g.findAir(m)
}

func (g *swimUpForBreath) Tick(m *mob.Mob) {
	if !m.Navigation().Done() {
		return
	}
	if g.found {
		m.Navigation().MoveTo(m, g.target, g.speed)
		return
	}
	g.findAir(m)
}

// depth is _determineApproximateDepth: blocks up to the first layer with air
// over water within 4 blocks sideways.
func (g *swimUpForBreath) depth(m *mob.Mob) int {
	tx := m.Tx()
	p := cube.PosFromVec3(m.Position())
	n := 0
	for y := p[1]; y < tx.Range()[1]; y++ {
		for dx := -g.radius; dx <= g.radius; dx++ {
			for dz := -g.radius; dz <= g.radius; dz++ {
				q := cube.Pos{p[0] + dx, y, p[2] + dz}
				if airAt(tx, q) && waterAt(tx, q.Side(cube.FaceDown)) {
					return n
				}
			}
		}
		n++
	}
	return 0
}

// findAir is _findAirPosition.
func (g *swimUpForBreath) findAir(m *mob.Mob) {
	tx := m.Tx()
	p := cube.PosFromVec3(m.Position())
	var cand []cube.Pos
	for dy := 0; dy <= 16 && len(cand) == 0; dy++ {
		for dx := -g.radius; dx <= g.radius; dx++ {
			for dz := -g.radius; dz <= g.radius; dz++ {
				q := cube.Pos{p[0] + dx, p[1] + dy, p[2] + dz}
				if inRange(tx, q) && airAt(tx, q) && waterAt(tx, q.Side(cube.FaceDown)) {
					cand = append(cand, q)
				}
			}
		}
	}
	g.found = len(cand) > 0
	if !g.found {
		g.target = m.Position().Add(mgl64.Vec3{0, 16, 0})
	} else {
		cur := cube.PosFromVec3(g.target)
		for _, q := range cand {
			if abs(q[0]-p[0])+abs(q[2]-p[2]) < 5 && q != cur {
				g.target = blockCenter(q)
			}
		}
		if g.target == (mgl64.Vec3{}) {
			g.target = blockCenter(cand[0])
		}
	}
	m.Navigation().MoveTo(m, g.target, g.speed)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (g *swimUpForBreath) AppendDebugInfo(b *strings.Builder) {
	fmt.Fprintf(b, "BreatheAir to %.1f %.1f %.1f", g.target[0], g.target[1], g.target[2])
}
