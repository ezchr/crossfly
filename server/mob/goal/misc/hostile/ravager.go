//go:build crossfly_full

package hostile

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// BreakBlocks is minecraft:break_blocks (ravager, BDS
// BreakBlocksComponent::breakNearbyBlocks): with mob griefing on, every
// block of breakable_blocks whose outline touches the mob's box grown by
// 0.2 (source: java/paper Ravager.aiStep) is destroyed with its drops.
type BreakBlocks struct {
	blocks kit.BlockList
}

func newBreakBlocks(key string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		Blocks json.RawMessage `json:"breakable_blocks"`
	}
	if !kit.IsNull(raw) {
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("%v: %w", key, err)
		}
	}
	l, err := kit.ParseBlockList(v.Blocks)
	if err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	return &BreakBlocks{blocks: l}, nil
}

// Attach ...
func (*BreakBlocks) Attach(*mob.Mob) {}

// Detach ...
func (*BreakBlocks) Detach(*mob.Mob) {}

// Breakable reports whether b is one of the breakable blocks.
func (c *BreakBlocks) Breakable(b world.Block) bool { return matchBlock(c.blocks, b) }

func (c *BreakBlocks) tick(m *mob.Mob) {
	if m.Dead() || len(c.blocks) == 0 || !combat.MobGriefing(m.Tx()) {
		return
	}
	tx := m.Tx()
	box := m.BBox().Grow(0.2)
	low, high := cube.PosFromVec3(box.Min()), cube.PosFromVec3(box.Max())
	for p := range cube.Range3D(low, high) {
		b := tx.Block(p)
		if _, air := b.(block.Air); air || !c.Breakable(b) {
			continue
		}
		touches := false
		for _, bb := range b.Model().BBox(p, tx) {
			if bb.Translate(p.Vec3()).IntersectsWith(box) {
				touches = true
				break
			}
		}
		// Plants have no collision box: their block space counts.
		if !touches && len(b.Model().BBox(p, tx)) == 0 {
			touches = cube.Box(float64(p[0]), float64(p[1]), float64(p[2]), float64(p[0]+1), float64(p[1]+1), float64(p[2]+1)).IntersectsWith(box)
		}
		if !touches {
			continue
		}
		destroy(tx, p, b)
	}
}

// destroy breaks b at p with particles and drops (BlockSource::destroyBlock).
func destroy(tx *world.Tx, p cube.Pos, b world.Block) {
	tx.AddParticle(p.Vec3Centre(), particle.BlockBreak{Block: b})
	tx.SetBlock(p, block.Air{}, nil)
	if br, ok := b.(block.Breakable); ok {
		for _, s := range br.BreakInfo().Drops(item.ToolNone{}, nil) {
			it := entity.NewItem(world.EntitySpawnOpts{Position: p.Vec3Centre()}, s)
			tx.AddEntity(it)
		}
	}
}

// RavagerBlocked is minecraft:ravager_blocked (BDS
// IllagerBeastBlockedDescription): when the ravager's melee hit is blocked
// by its target's shield, the target is pushed away with
// knockback_strength (3; source: java/paper Ravager.strongKnockback, 4 over
// the distance) and one of reaction_choices (weight, value trigger, may be
// empty) fires on the ravager. Dragonfly reports a shield block only to the
// victim: the ravager sees it through player.ShieldBlockState right after
// its goals ran, while its target is within 4 blocks and it attacks
// (aggressive or delayed_attack).
type RavagerBlocked struct {
	strength float64
	choices  []reaction
	cooldown int64
}

type reaction struct {
	weight  int
	trigger kit.Trigger
}

func newRavagerBlocked(key string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		Strength *float64 `json:"knockback_strength"`
		Choices  []struct {
			Weight *int            `json:"weight"`
			Value  json.RawMessage `json:"value"`
		} `json:"reaction_choices"`
	}
	if !kit.IsNull(raw) {
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("%v: %w", key, err)
		}
	}
	c := &RavagerBlocked{strength: kit.Or(v.Strength, 3)}
	for _, ch := range v.Choices {
		t, err := kit.ParseTrigger(ch.Value)
		if err != nil {
			return nil, fmt.Errorf("%v: reaction_choices: %w", key, err)
		}
		c.choices = append(c.choices, reaction{weight: kit.Or(ch.Weight, 1), trigger: t})
	}
	return c, nil
}

// Attach ...
func (*RavagerBlocked) Attach(*mob.Mob) {}

// Detach ...
func (*RavagerBlocked) Detach(*mob.Mob) {}

func (c *RavagerBlocked) tick(m *mob.Mob) {
	if m.Dead() || m.Ticks() < c.cooldown {
		return
	}
	t, ok := m.Target()
	if !ok {
		return
	}
	p, ok := t.(*player.Player)
	if !ok || kit.DistSq(p.Position(), m.Position()) > 16 {
		return
	}
	if !m.Meta().Aggressive && !m.ActorFlag(protocol.EntityDataFlagDelayedAttack) {
		return
	}
	blocked, damaged := p.ShieldBlockState()
	if !blocked && !damaged {
		return
	}
	c.cooldown = m.Ticks() + 20
	c.Blocked(m, p)
}

// Blocked runs the reaction to a blocked hit on t.
func (c *RavagerBlocked) Blocked(m *mob.Mob, t world.Entity) {
	if l, ok := t.(entity.Living); ok {
		d := t.Position().Sub(m.Position())
		d[1] = 0
		dist := math.Max(d.Len(), 0.001)
		l.KnockBack(m.Position(), c.strength/math.Max(dist, 1), 0.2)
	}
	total := 0
	for _, ch := range c.choices {
		total += max(ch.weight, 0)
	}
	if total == 0 {
		return
	}
	n := m.Rand().IntN(total)
	for _, ch := range c.choices {
		if n -= max(ch.weight, 0); n < 0 {
			ch.trigger.Fire(m, kit.OtherCtx(m, t))
			return
		}
	}
}
