//go:build crossfly_full

package hostile

import (
	"encoding/json"

	"github.com/df-mc/dragonfly/server/mob"
)

// Factories and parsed values for the external tests.
var (
	NewTeleport           = newTeleport
	NewLookedAt           = newLookedAt
	NewBreakBlocks        = newBreakBlocks
	NewRavagerBlocked     = newRavagerBlocked
	NewCustomHitTest      = newCustomHitTest
	NewGroupSize          = newGroupSize
	NewReflectProjectiles = newReflectProjectiles
)

// Build runs a factory on JSON text.
func Build(f func(string, json.RawMessage) (mob.Component, error), key, raw string) (mob.Component, error) {
	return f(key, json.RawMessage(raw))
}

// Params returns the parsed values of a component of this package.
func Params(c mob.Component) map[string]float64 {
	b := func(v bool) float64 {
		if v {
			return 1
		}
		return 0
	}
	switch c := c.(type) {
	case *Teleport:
		return map[string]float64{"random": b(c.random), "min": float64(c.minTime), "max": float64(c.maxTime),
			"cube_x": c.cube[0], "cube_y": c.cube[1], "target_distance": c.targetDist, "target_chance": c.targetChance,
			"light": c.lightChance, "dark": c.darkChance, "projectile": b(c.onProjectile)}
	case *LookedAt:
		return map[string]float64{"radius": c.radius, "fov": c.fov, "scale": b(c.scale), "min": float64(c.minDuration),
			"locations": float64(len(c.locations)), "players_only": b(c.playersOnly)}
	case *RavagerBlocked:
		return map[string]float64{"strength": c.strength, "choices": float64(len(c.choices))}
	case *GroupSize:
		return map[string]float64{"radius": c.radius}
	case *BreakBlocks:
		return map[string]float64{"blocks": float64(len(c.blocks))}
	}
	return nil
}

// SetTargetMode is LookedAt's set_target.
func SetTargetMode(c *LookedAt) string { return c.setTarget }
