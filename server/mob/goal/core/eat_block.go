//go:build crossfly_full

package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	ccore "github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
)

// eatBlock is minecraft:behavior.eat_block: now and then eat the block the
// mob stands in or on (a sheep eating grass), replacing it, then fire
// on_eat (BDS EatBlockGoal, "EatTile").
//
//	success_chance (Molang, default 0.02) per tick to start, time_until_eat
//	(1.8 s) of head-down time, eat_and_replace_block_pairs [{eat_block,
//	replace_block}], on_eat. Flags move|look|jump.
type eatBlock struct {
	mob.GoalBase
	chance ccore.Expr
	time   int
	pairs  []eatPair
	onEat  ccore.Trigger

	left int
}

type eatPair struct {
	eat, replace string
}

// legacyBlocks maps the block names entity JSON uses to Dragonfly's.
var legacyBlocks = map[string]string{
	"minecraft:grass":     "minecraft:grass_block",
	"minecraft:tallgrass": "minecraft:short_grass",
}

func blockName(n string) string {
	n = strings.ToLower(strings.TrimSpace(n))
	if !strings.Contains(n, ":") {
		n = "minecraft:" + n
	}
	if to, ok := legacyBlocks[n]; ok {
		return to
	}
	return n
}

var (
	blockCacheMu sync.Mutex
	blockCache   = map[string]world.Block{}
)

// blockByName returns the default state of a block name. The block registry
// must be finalised (a world exists), so goals resolve blocks when they use
// them, not in the factory.
func blockByName(n string) (world.Block, bool) {
	n = blockName(n)
	if n == "minecraft:air" {
		return block.Air{}, true
	}
	blockCacheMu.Lock()
	defer blockCacheMu.Unlock()
	if b, ok := blockCache[n]; ok {
		return b, b != nil
	}
	b, ok := lookupBlock(n)
	blockCache[n] = b
	return b, ok
}

func lookupBlock(n string) (world.Block, bool) {
	props, _ := world.DefaultBlockRegistry.DefaultProperties(n)
	if b, ok := world.BlockByName(n, props); ok {
		return b, true
	}
	for _, b := range world.Blocks() {
		if name, _ := b.EncodeBlock(); name == n {
			return b, true
		}
	}
	return nil, false
}

func newEatBlock(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Chance json.RawMessage `json:"success_chance"`
		Time   *float64        `json:"time_until_eat"`
		Pairs  []struct {
			Eat     string `json:"eat_block"`
			Replace string `json:"replace_block"`
		} `json:"eat_and_replace_block_pairs"`
		OnEat ccore.Trigger `json:"on_eat"`
	}{}
	if err := spec.Decode(&j); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	g := &eatBlock{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook | mob.FlagJump)},
		chance: ccore.Const(0.02), time: 36, onEat: j.OnEat}
	if len(j.Chance) > 0 {
		e, err := ccore.ParseExpr(j.Chance)
		if err != nil {
			return nil, fmt.Errorf("%v: %w", spec.Key, err)
		}
		g.chance = e
	}
	if j.Time != nil {
		g.time = int(*j.Time*20 + 0.5)
	}
	for _, p := range j.Pairs {
		g.pairs = append(g.pairs, eatPair{eat: blockName(p.Eat), replace: blockName(p.Replace)})
	}
	return g, nil
}

// edible returns the pair for the block at pos.
func (g *eatBlock) edible(tx *world.Tx, pos cube.Pos) (eatPair, bool) {
	name, _ := tx.Block(pos).EncodeBlock()
	for _, p := range g.pairs {
		if p.eat == name {
			return p, true
		}
	}
	return eatPair{}, false
}

func (g *eatBlock) CanUse(m *mob.Mob) bool {
	if !hasNavigation(m) || m.Rand().Float64() >= g.chance.Eval(m) {
		return false
	}
	feet := floorPos(m.Position())
	if _, ok := g.edible(m.Tx(), feet); ok {
		return true
	}
	_, ok := g.edible(m.Tx(), feet.Side(cube.FaceDown))
	return ok
}

func (g *eatBlock) CanContinueToUse(*mob.Mob) bool { return g.left > 0 }

func (g *eatBlock) Start(m *mob.Mob) {
	g.left = g.time
	ccore.ShowAction(m, ccore.EatGrassAction{})
	m.Navigation().Stop(m)
}

func (g *eatBlock) Stop(*mob.Mob) { g.left = 0 }

func (g *eatBlock) Tick(m *mob.Mob) {
	if g.left = max(g.left-1, 0); g.left != 0 {
		return
	}
	tx := m.Tx()
	pos := floorPos(m.Position())
	p, ok := g.edible(tx, pos)
	if !ok {
		pos = pos.Side(cube.FaceDown)
		if p, ok = g.edible(tx, pos); !ok {
			return
		}
	}
	if rep, ok := blockByName(p.replace); ok {
		tx.AddParticle(pos.Vec3Centre(), particle.BlockBreak{Block: tx.Block(pos)})
		tx.SetBlock(pos, rep, nil)
	}
	g.onEat.Fire(m, nil)
}

func (g *eatBlock) AppendDebugInfo(b *strings.Builder) { fmt.Fprintf(b, "EatTile %d", g.left) }
