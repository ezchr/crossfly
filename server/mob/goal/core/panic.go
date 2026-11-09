//go:build crossfly_full

package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	ccore "github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/go-gl/mathgl/mgl64"
)

// floatGoal is minecraft:behavior.float: swim up in water and lava by jumping
// (BDS FloatGoal: jump with 80% chance each tick while in liquid).
//
//	chance_per_tick_to_float 0.8 (1.26). Flags jump.
type floatGoal struct {
	mob.GoalBase
	chance float64
}

func newFloat(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Chance float64 `json:"chance_per_tick_to_float"`
	}{Chance: 0.8}
	if err := spec.Decode(&j); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	return &floatGoal{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagJump)}, chance: j.Chance}, nil
}

func (g *floatGoal) CanUse(m *mob.Mob) bool {
	p := m.Physics()
	if !p.InWater && !p.InLava {
		return false
	}
	if m.Meta().Sitting {
		m.UpdateMeta(func(meta *mob.Meta) { meta.Sitting = false })
	}
	return true
}

func (g *floatGoal) Tick(m *mob.Mob) {
	if m.Rand().Float64() < g.chance {
		m.JumpControl().Jump()
	}
}

// defaultPanicCauses is the damage that makes a mob panic without
// damage_sources (BDS PanicGoalConstants::POST_SUMMER_24_UPDATE_DEFAULT_
// PANIC_DAMAGE_CAUSES is static data missing from the dump; this is the
// documented list: attacks, explosions, fire, freezing, lightning, magic).
var defaultPanicCauses = []string{"entity_attack", "entity_explosion", "block_explosion", "projectile", "fire",
	"fire_tick", "lava", "magma", "campfire", "soul_campfire", "freezing", "temperature", "lightning", "magic",
	"sonic_boom", "ram_attack", "contact"}

// panicGoal is minecraft:behavior.panic: run away after being hurt (BDS
// PanicGoal).
//
//	speed_multiplier 1, force (panic without being hurt), ignore_mob_damage,
//	prefer_water (run to water when not in it), damage_sources ("all" or a
//	list of causes), panic_sound, sound_interval. Flags move.
//
// It starts within 40 ticks of the last damage: hurt by a mob (unless
// ignore_mob_damage), or by one of damage_sources. It runs until the
// navigation is done.
type panicGoal struct {
	mob.GoalBase
	speed       float64
	force       bool
	ignoreMob   bool
	preferWater bool
	all         bool
	causes      []string
	sound       string
	interval    soundInterval
	attacker    *mobOrNil
	wanted      mgl64.Vec3
}

// mobOrNil is the attacker the panic runs from.
type mobOrNil struct{ pos mgl64.Vec3 }

func newPanic(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Speed       float64         `json:"speed_multiplier"`
		Force       bool            `json:"force"`
		IgnoreMob   bool            `json:"ignore_mob_damage"`
		PreferWater bool            `json:"prefer_water"`
		Sources     json.RawMessage `json:"damage_sources"`
		Sound       string          `json:"panic_sound"`
		Interval    *soundInterval  `json:"sound_interval"`
	}{Speed: 1}
	if err := spec.Decode(&j); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	g := &panicGoal{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}, speed: j.Speed, force: j.Force,
		ignoreMob: j.IgnoreMob, preferWater: j.PreferWater, sound: j.Sound}
	if j.Interval != nil {
		g.interval = *j.Interval
	}
	if src := bytes.TrimSpace(j.Sources); len(src) > 0 {
		var one string
		var list []string
		if json.Unmarshal(src, &one) == nil {
			list = []string{one}
		} else if err := json.Unmarshal(src, &list); err != nil {
			return nil, fmt.Errorf("%v: damage_sources: %w", spec.Key, err)
		}
		g.causes = list
	} else {
		g.causes = defaultPanicCauses
	}
	g.all = slices.Contains(g.causes, "all")
	return g, nil
}

func (g *panicGoal) CanUse(m *mob.Mob) bool {
	g.attacker = nil
	if e, t, ok := m.LastHurtBy(); ok && m.Ticks()-t <= 100 && alive(m.Tx(), e) {
		g.attacker = &mobOrNil{pos: e.Position()}
	}
	if !g.force {
		if !hasNavigation(m) {
			return false
		}
		last, ok := ccore.LastHurtOf(m)
		if !ok {
			return false
		}
		if g.attacker != nil {
			if g.ignoreMob {
				return false
			}
		} else if !g.all && !slices.Contains(g.causes, last.Cause) || g.all && last.Cause == "none" {
			return false
		}
		if m.Ticks()-last.Tick > 40 {
			return false
		}
	}
	pos, ok := g.pathEnd(m)
	if !ok {
		return false
	}
	g.wanted = pos
	if m.Meta().Sitting {
		m.UpdateMeta(func(meta *mob.Meta) { meta.Sitting = false })
	}
	return true
}

// pathEnd is PanicGoal::_tryGeneratePathEnd.
func (g *panicGoal) pathEnd(m *mob.Mob) (mgl64.Vec3, bool) {
	tx := m.Tx()
	if g.preferWater {
		inWater := false
		if l, ok := tx.Liquid(cube.PosFromVec3(m.Position())); ok {
			_, inWater = l.(block.Water)
		}
		if g.attacker == nil || !inWater {
			if hasNavigation(m) {
				base := cube.PosFromVec3(m.Position())
				best, found := -99999.0, false
				var bestPos cube.Pos
				// BDS scans x -1..10, y -4..0, z -10..10.
				for dy := -4; dy <= 0; dy++ {
					for dx := -1; dx <= 10; dx++ {
						for dz := -10; dz <= 10; dz++ {
							p := base.Add(cube.Pos{dx, dy, dz})
							l, ok := tx.Liquid(p)
							if !ok {
								continue
							}
							if _, water := l.(block.Water); !water {
								continue
							}
							d := float64(dx*dx + dy*dy + dz*dz)
							if v := WalkTargetValue(m, p) - d*0.01; v > best {
								best, bestPos, found = v, p, true
							}
						}
					}
				}
				if found {
					return mgl64.Vec3{float64(bestPos[0]), float64(bestPos[1]), float64(bestPos[2])}, true
				}
			}
		} else {
			away := m.Position().Sub(g.attacker.pos)
			if p, ok := RandomPos(m, 5, 4, 0, &away, 10); ok {
				return p, true
			}
		}
	}
	if g.attacker != nil {
		away := m.Position().Sub(g.attacker.pos)
		return RandomPos(m, 5, 4, 0, &away, 10)
	}
	return RandomPos(m, 5, 4, 0, nil, 10)
}

func (g *panicGoal) Start(m *mob.Mob) {
	m.Navigation().MoveTo(m, g.wanted.Add(mgl64.Vec3{0.5, 0, 0.5}), g.speed)
}

func (g *panicGoal) CanContinueToUse(m *mob.Mob) bool {
	return hasNavigation(m) && !m.Navigation().Done()
}

func (g *panicGoal) Stop(m *mob.Mob) {
	m.Navigation().Stop(m)
	m.Navigation().SetSpeed(1)
}

func (g *panicGoal) Tick(m *mob.Mob) {
	if g.sound != "" && g.interval.due(m) {
		ccore.PlayMobSound(m, g.sound)
	}
}

func (g *panicGoal) AppendDebugInfo(b *strings.Builder) {
	fmt.Fprintf(b, "Panic to %.0f %.0f %.0f", g.wanted[0], g.wanted[1], g.wanted[2])
}
