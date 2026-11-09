//go:build crossfly_full

package misc

import (
	"encoding/json"
	"fmt"

	"github.com/df-mc/dragonfly/server/mob"
)

var factories = map[string]mob.GoalFactory{
	"minecraft:behavior.timer_flag_1":         newTimerFlag(timerFlags["minecraft:behavior.timer_flag_1"]),
	"minecraft:behavior.ram_attack":           newRamAttack,
	"minecraft:behavior.charge_attack":        newChargeAttack,
	"minecraft:behavior.knockback_roar":       newKnockbackRoar,
	"minecraft:behavior.move_to_block":        newMoveToBlock,
	"minecraft:behavior.stomp_turtle_egg":     newStompTurtleEgg,
	"minecraft:behavior.avoid_block":          newAvoidBlock,
	"minecraft:behavior.move_to_random_block": newMoveToRandomBlock,
	"minecraft:behavior.move_towards_target":  newMoveTowardsTarget,
	"minecraft:behavior.target_when_pushed":   newTargetWhenPushed,
	"minecraft:behavior.hold_ground":          newHoldGround,
	"minecraft:behavior.follow_mob":           newFollowMob,
	"minecraft:behavior.look_at_target":       newLookAtTarget,
	"minecraft:behavior.send_event":           newSendEvent,
	"minecraft:behavior.summon_entity":        newSummonEntity,
}

// Goal builds one of the package's goals from JSON.
func Goal(key, raw string) (mob.Goal, error) {
	f, ok := factories[key]
	if !ok {
		return nil, fmt.Errorf("no factory %v", key)
	}
	spec, err := mob.ParseGoalSpec(key, json.RawMessage(raw))
	if err != nil {
		return nil, err
	}
	return f(spec)
}

// Params returns parsed values of a goal for default checks.
func Params(g mob.Goal) map[string]any {
	switch g := g.(type) {
	case *timerFlag:
		return map[string]any{"cdMin": g.cooldown.Min, "cdMax": g.cooldown.Max, "durMin": g.length.Min, "flags": g.Flags()}
	case *ramAttack:
		return map[string]any{"run": g.runSpeed, "ram": g.ramSpeed, "min": g.minDist, "dist": g.ramDist, "force": g.force, "height": g.height, "baby": g.baby, "cdMin": g.cooldown.Min, "cdMax": g.cooldown.Max, "flags": g.Flags()}
	case *chargeAttack:
		return map[string]any{"min": g.minDist, "rate": g.rate, "speed": g.speed, "flags": g.Flags()}
	case *knockbackRoar:
		return map[string]any{"duration": g.duration, "attack": g.attackAt, "damage": g.damage, "h": g.h, "v": g.v, "range": g.rng, "cooldown": g.cooldown, "flags": g.Flags()}
	case *moveToBlock:
		return map[string]any{"interval": g.interval, "chance": g.chance, "range": g.rangeXZ, "height": g.rangeY, "radius": g.radius, "stay": g.stay, "speed": g.speed, "flags": g.Flags()}
	case *stompEgg:
		return map[string]any{"interval": g.interval, "range": g.rangeXZ, "height": g.rangeY, "radius": g.radius, "blocks": len(g.blocks)}
	case *avoidBlock:
		return map[string]any{"interval": g.interval, "range": g.rangeXZ, "height": g.rangeY, "walk": g.walk, "sprint": g.sprint, "sMin": g.soundInterval.Min, "sMax": g.soundInterval.Max, "flags": g.Flags()}
	case *moveToRandomBlock:
		return map[string]any{"distance": g.distance, "radius": g.radius, "speed": g.speed}
	case *moveTowardsTarget:
		return map[string]any{"radius": g.radius, "speed": g.speed, "flags": g.Flags()}
	case *targetWhenPushed:
		return map[string]any{"chance": g.chance, "filters": len(g.filters)}
	case *holdGround:
		return map[string]any{"radius": g.radius, "broadcast": g.broadcast, "bcRange": g.bcRange, "flags": g.Flags()}
	case *followMob:
		return map[string]any{"range": g.rng, "stop": g.stop, "speed": g.speed, "flags": g.Flags()}
	case *lookAtTarget:
		return map[string]any{"min": g.time.Min, "max": g.time.Max, "flags": g.Flags()}
	case *sendEvent:
		c := g.choices[0]
		return map[string]any{"maxR": c.maxR, "weight": c.weight, "cast": c.cast, "steps": len(g.seqs[c])}
	case *summonEntity:
		c := g.choices[0]
		s := g.seqs[c][0]
		return map[string]any{"maxR": c.maxR, "count": s.count, "size": s.size, "casting": c.casting}
	}
	return nil
}

// TrailPositions is trailPositions, for the tests.
var TrailPositions = trailPositions
