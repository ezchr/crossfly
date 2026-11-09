//go:build crossfly_full

package misc

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// MobEffect is minecraft:mob_effect (BDS MobEffectComponent::tick): entities
// within effect_range of the mob's box that pass entity_filter and do not
// have the effect yet get it for effect_time. With cooldown_time the mob
// applies it at most once per cooldown (pufferfish poison, warden darkness).
//
//	mob_effect     effect name
//	effect_range   0.2 blocks around the box
//	effect_time    10 s (BDS default 200 ticks)
//	cooldown_time  0 s
//	ambient        false
//	entity_filter  filters with the entity as "other"
type MobEffect struct {
	effect   effect.Type
	name     string
	rng      float64
	duration int
	cooldown int
	ambient  bool
	filter   mob.Filter

	next int64 // mob tick the cooldown ends
}

func newMobEffect(key string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		MobEffect    string          `json:"mob_effect"`
		EffectRange  *float64        `json:"effect_range"`
		EffectTime   *float64        `json:"effect_time"`
		CooldownTime float64         `json:"cooldown_time"`
		Ambient      bool            `json:"ambient"`
		EntityFilter json.RawMessage `json:"entity_filter"`
	}{}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	c := &MobEffect{name: v.MobEffect, rng: 0.2, duration: 200, cooldown: ticks(v.CooldownTime), ambient: v.Ambient}
	c.effect, _ = EffectByName(v.MobEffect)
	if v.EffectRange != nil {
		c.rng = *v.EffectRange
	}
	if v.EffectTime != nil {
		c.duration = ticks(*v.EffectTime)
	}
	f, err := mob.CompileFilter(v.EntityFilter)
	if err != nil {
		return nil, fmt.Errorf("%v: entity_filter: %w", key, err)
	}
	c.filter = f
	return c, nil
}

// Attach ...
func (*MobEffect) Attach(*mob.Mob) {}

// Detach ...
func (*MobEffect) Detach(*mob.Mob) {}

// Tick applies the effect.
func (c *MobEffect) Tick(m *mob.Mob) {
	if c.effect == nil || m.Dead() {
		return
	}
	if c.cooldown > 0 {
		if m.Ticks() < c.next {
			return
		}
		c.next = m.Ticks() + int64(c.cooldown)
	}
	b := m.BBox()
	r := c.rng
	for _, e := range entitiesIn(m, b.Min()[0]-r, b.Min()[1]-r, b.Min()[2]-r, b.Max()[0]+r, b.Max()[1]+r, b.Max()[2]+r) {
		l, ok := livingTarget(e)
		if !ok || !canReceiveEffects(e) {
			continue
		}
		if !c.filter.Test(otherCtx(m, e)) {
			continue
		}
		if hasEffect(l, c.effect) {
			continue
		}
		l.AddEffect(NewEffect(c.effect, 0, c.duration, c.ambient, true))
	}
}

// EncodeNBT ...
func (c *MobEffect) EncodeNBT() map[string]any { return map[string]any{"Cooldown": c.next} }

// DecodeNBT ...
func (c *MobEffect) DecodeNBT(_ *mob.Mob, d map[string]any) {
	if v, ok := d["Cooldown"].(int64); ok {
		c.next = v
	}
}

// canReceiveEffects is Actor::canReceiveMobEffectsFromGameplay: creative and
// spectator players do not.
func canReceiveEffects(e world.Entity) bool {
	if p, ok := e.(*player.Player); ok {
		return p.GameMode().AllowsTakingDamage()
	}
	return true
}

func hasEffect(l interface{ Effects() []effect.Effect }, t effect.Type) bool {
	for _, e := range l.Effects() {
		if e.Type() == t {
			return true
		}
	}
	return false
}

// SpellEffects is minecraft:spell_effects (BDS SpellEffectsComponent): when
// it is added, add_effects are applied and remove_effects removed.
//
//	add_effects     [{effect, duration (s, default 0), amplifier, ambient,
//	                  visible (true), display_on_screen_animation}]
//	remove_effects  a name or a list of names
type SpellEffects struct {
	add    []spellEffect
	remove []effect.Type
}

type spellEffect struct {
	t         effect.Type
	ticks     int
	amplifier int
	ambient   bool
	visible   bool
}

func newSpellEffects(key string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		Add []struct {
			Effect    string   `json:"effect"`
			Duration  *float64 `json:"duration"`
			Amplifier int      `json:"amplifier"`
			Ambient   bool     `json:"ambient"`
			Visible   *bool    `json:"visible"`
		} `json:"add_effects"`
		Remove json.RawMessage `json:"remove_effects"`
	}{}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	c := &SpellEffects{}
	for _, a := range v.Add {
		t, ok := EffectByName(a.Effect)
		if !ok {
			continue
		}
		e := spellEffect{t: t, amplifier: a.Amplifier, ambient: a.Ambient, visible: true}
		if a.Duration != nil {
			e.ticks = ticks(*a.Duration)
		}
		if a.Visible != nil {
			e.visible = *a.Visible
		}
		c.add = append(c.add, e)
	}
	var names []string
	if r := bytes.TrimSpace(v.Remove); len(r) > 0 {
		if r[0] == '"' {
			var s string
			_ = json.Unmarshal(r, &s)
			names = []string{s}
		} else if err := json.Unmarshal(r, &names); err != nil {
			return nil, fmt.Errorf("%v: remove_effects: %w", key, err)
		}
	}
	for _, n := range names {
		if t, ok := EffectByName(n); ok {
			c.remove = append(c.remove, t)
		}
	}
	return c, nil
}

// Attach applies the effects (BDS applies them when the component is
// added; a removed and re-added group applies them again).
func (c *SpellEffects) Attach(m *mob.Mob) {
	for _, t := range c.remove {
		m.RemoveEffect(t)
	}
	for _, e := range c.add {
		m.AddEffect(NewEffect(e.t, e.amplifier, e.ticks, e.ambient, e.visible))
	}
}

// Detach ...
func (*SpellEffects) Detach(*mob.Mob) {}
