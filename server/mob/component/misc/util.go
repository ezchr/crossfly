//go:build crossfly_full

package misc

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// effects maps BDS MobEffect names (MobEffect::getByName, plus the legacy
// names entity JSON still uses) to Dragonfly effect types. Effects Dragonfly
// does not have (bad_omen, raid_omen, village_hero, infested, oozing,
// weaving, wind_charged, breath_of_the_nautilus, trial_omen) are missing.
var effects = map[string]effect.Type{
	"speed":           effect.Speed,
	"slowness":        effect.Slowness,
	"haste":           effect.Haste,
	"mining_fatigue":  effect.MiningFatigue,
	"strength":        effect.Strength,
	"instant_health":  effect.InstantHealth,
	"heal":            effect.InstantHealth,
	"instant_damage":  effect.InstantDamage,
	"harm":            effect.InstantDamage,
	"jump_boost":      effect.JumpBoost,
	"jump":            effect.JumpBoost,
	"nausea":          effect.Nausea,
	"regeneration":    effect.Regeneration,
	"resistance":      effect.Resistance,
	"fire_resistance": effect.FireResistance,
	"water_breathing": effect.WaterBreathing,
	"invisibility":    effect.Invisibility,
	"blindness":       effect.Blindness,
	"night_vision":    effect.NightVision,
	"hunger":          effect.Hunger,
	"weakness":        effect.Weakness,
	"poison":          effect.Poison,
	"wither":          effect.Wither,
	"health_boost":    effect.HealthBoost,
	"absorption":      effect.Absorption,
	"saturation":      effect.Saturation,
	"levitation":      effect.Levitation,
	"fatal_poison":    effect.FatalPoison,
	"conduit_power":   effect.ConduitPower,
	"slow_falling":    effect.SlowFalling,
	"darkness":        effect.Darkness,
}

// EffectByName returns the Dragonfly effect type of a BDS effect name
// ("poison", "minecraft:poison", "heal"), if Dragonfly has it.
func EffectByName(name string) (effect.Type, bool) {
	t, ok := effects[strings.TrimPrefix(strings.ToLower(name), "minecraft:")]
	return t, ok
}

// NewEffect makes an effect of t lasting ticks at amplifier (level =
// amplifier+1). Instant effects ignore the duration.
func NewEffect(t effect.Type, amplifier, ticks int, ambient, visible bool) effect.Effect {
	lt, lasting := t.(effect.LastingType)
	if !lasting {
		return effect.NewInstant(t, amplifier+1)
	}
	d := time.Duration(ticks) * time.Second / 20
	var e effect.Effect
	if ambient {
		e = effect.NewAmbient(lt, amplifier+1, d)
	} else {
		e = effect.New(lt, amplifier+1, d)
	}
	if !visible {
		e = e.WithoutParticles()
	}
	return e
}

// ticks converts seconds to ticks.
func ticks(seconds float64) int { return sense.Ticks(seconds) }

// isNull reports whether raw is absent or JSON null.
func isNull(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) == 0 || bytes.Equal(t, []byte("null"))
}

// decode unmarshals raw into v when raw is present.
func decode(raw json.RawMessage, v any) error {
	if isNull(raw) {
		return nil
	}
	return json.Unmarshal(raw, v)
}

// otherCtx is m's filter context with e as "other" (and player when it is
// one).
func otherCtx(m *mob.Mob, e world.Entity) *mob.FilterContext {
	ctx := m.Ctx()
	ctx.Other = e
	if t, ok := m.Target(); ok {
		ctx.Target = t
	}
	if _, ok := e.(*player.Player); ok {
		ctx.Player = e
	}
	return ctx
}

// entitiesIn returns the entities other than m whose box touches the box
// (BDS BlockSource::fetchEntities tests AABB intersection; Dragonfly's
// EntitiesWithin tests positions, so the query is grown first).
func entitiesIn(m *mob.Mob, minX, minY, minZ, maxX, maxY, maxZ float64) []world.Entity {
	box := boxOf(minX, minY, minZ, maxX, maxY, maxZ)
	return entitiesTouching(m, box)
}

func entitiesTouching(m *mob.Mob, box cube.BBox) []world.Entity {
	var out []world.Entity
	q := boxOf(box.Min()[0]-3, box.Min()[1]-4, box.Min()[2]-3, box.Max()[0]+3, box.Max()[1]+1, box.Max()[2]+3)
	for e := range m.Tx().EntitiesWithin(q) {
		if e.H() != m.H() && sense.BBox(e).IntersectsWith(box) {
			out = append(out, e)
		}
	}
	return out
}

// livingTarget reports whether e can be hurt (living and alive).
func livingTarget(e world.Entity) (entity.Living, bool) {
	l, ok := e.(entity.Living)
	if !ok || l.Dead() || e.H().Closed() {
		return nil, false
	}
	return l, true
}

// attackSource returns a damage source for a BDS cause dealt by m:
// entity_attack is an attack by m, other causes use their own sources.
func attackSource(m *mob.Mob, cause string) world.DamageSource {
	switch cause {
	case "", "entity_attack", "none":
		return entity.AttackDamageSource{Attacker: m}
	}
	return causeSource(cause)
}

// center is the middle of e's box.
func center(e world.Entity) mgl64.Vec3 { return sense.Center(e) }
