//go:build crossfly_full

package physics

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// pushPreset is one entry of minecraft:pushable_by_entity "presets" (1.26): a push mode the mob has against the
// entities its filters pass. Only push_mode "ball" exists in the vanilla files (sulfur cubes holding a block).
type pushPreset struct {
	filter                      mob.Filter
	ball                        bool
	minDist, maxDist            float64
	overlap                     bool
	scaleSelf                   float64
	kickScale, minKick, maxKick float64
	verticalKick                float64
	sound                       bool
	soundImpulse                float64
	soundCooldown               int
}

// pushPresets is minecraft:pushable_by_entity with presets: the mob stays pushable, and pushers that a ball
// preset applies to kick it like a ball.
type pushPresets struct {
	presets []pushPreset
	// last holds where each entity in reach was a tick ago, for its speed;
	// next is filled this tick.
	last, next    map[*world.EntityHandle]mgl64.Vec3
	soundCooldown int
}

// newPushPresets parses the presets of minecraft:pushable_by_entity; ok is false without any.
func newPushPresets(raw json.RawMessage) (c mob.Component, ok bool, err error) {
	var v struct {
		Presets []struct {
			Filters       json.RawMessage `json:"filters"`
			PushMode      string          `json:"push_mode"`
			MinDistance   float64         `json:"min_distance"`
			MaxDistance   *float64        `json:"max_distance"`
			Overlap       bool            `json:"require_collision_overlap"`
			ScaleSelf     *float64        `json:"push_scale_self"`
			KickScale     *float64        `json:"kick_speed_scale"`
			MinKick       float64         `json:"min_kick_speed"`
			MaxKick       *float64        `json:"max_kick_speed"`
			VerticalKick  float64         `json:"vertical_kick_multiplier"`
			PlaySound     bool            `json:"play_sound"`
			SoundImpulse  float64         `json:"play_sound_impulse_threshold"`
			SoundCooldown float64         `json:"play_sound_cooldown_in_seconds"`
		} `json:"presets"`
	}
	if err := decode(raw, &v); err != nil || len(v.Presets) == 0 {
		return nil, false, err
	}
	or := func(p *float64, d float64) float64 {
		if p == nil {
			return d
		}
		return *p
	}
	p := &pushPresets{last: map[*world.EntityHandle]mgl64.Vec3{}, next: map[*world.EntityHandle]mgl64.Vec3{}}
	for _, r := range v.Presets {
		var f mob.Filter
		if len(r.Filters) > 0 {
			if f, err = mob.CompileFilter(r.Filters); err != nil {
				return nil, true, fmt.Errorf("minecraft:pushable_by_entity: presets: %w", err)
			}
		}
		p.presets = append(p.presets, pushPreset{filter: f, ball: r.PushMode == "ball", minDist: r.MinDistance,
			maxDist: or(r.MaxDistance, 1), overlap: r.Overlap, scaleSelf: or(r.ScaleSelf, 1), kickScale: or(r.KickScale, 1),
			minKick: r.MinKick, maxKick: or(r.MaxKick, math.MaxFloat64), verticalKick: r.VerticalKick, sound: r.PlaySound,
			soundImpulse: r.SoundImpulse, soundCooldown: int(r.SoundCooldown * 20)})
	}
	return p, true, nil
}

// Attach makes the mob pushable, as the plain component does.
func (p *pushPresets) Attach(m *mob.Mob) { setPushable(stateOf(m), true) }

// Detach ...
func (p *pushPresets) Detach(m *mob.Mob) {
	setPushable(stateOf(m), false)
	clear(p.last)
}

// Tick kicks the mob away from every pusher a ball preset applies to: those within max_distance of it and
// overlapping it vertically, by how fast they move. The Bedrock push system's body is not in the dumps, so the
// formula is Java's SulfurCube::playerPush, run with the preset's numbers (push_scale_self for Java's
// 1 - knockback resistance, kick_speed_scale, min/max_kick_speed, vertical_kick_multiplier).
// Java fallback: no Bedrock data (the ball push formula).
func (p *pushPresets) Tick(m *mob.Mob) {
	if p.soundCooldown > 0 {
		p.soundCooldown--
	}
	reach := 0.0
	for _, pr := range p.presets {
		if pr.ball {
			reach = max(reach, pr.maxDist)
		}
	}
	if reach == 0 {
		return
	}
	tx := m.Tx()
	pos, box := m.Position(), m.BBox()
	seen := p.next
	clear(seen)
	for e := range tx.EntitiesWithin(box.Grow(reach + 1)) {
		if e.H() == m.H() {
			continue
		}
		ep := e.Position()
		last, moved := p.last[e.H()]
		seen[e.H()] = ep
		for _, pr := range p.presets {
			if !pr.ball || !pr.applies(m, e) {
				continue
			}
			eb := e.H().Type().BBox(e).Translate(ep)
			if eb.Min()[1] > box.Max()[1] || eb.Max()[1] <= box.Min()[1] {
				continue
			}
			if pr.overlap && !eb.IntersectsWith(box) {
				continue
			}
			away := mgl64.Vec3{pos[0] - ep[0], 0, pos[2] - ep[2]}
			if d := away.Len(); d < pr.minDist || d > pr.maxDist || d < 1e-4 {
				continue
			}
			speed := 0.0
			if moved {
				speed = ep.Sub(last).Len()
			}
			speed = min(max(speed*2*pr.kickScale, pr.minKick), pr.maxKick)
			dir := away.Normalize().Mul(pr.scaleSelf)
			up := 0.0
			if m.OnGround() {
				up = pr.scaleSelf * pr.verticalKick
			}
			kick := mgl64.Vec3{dir[0], up, dir[2]}.Mul(speed)
			if kick.LenSqr() == 0 {
				break
			}
			if pr.sound && p.soundCooldown <= 0 && kick.LenSqr() > pr.soundImpulse*pr.soundImpulse {
				p.soundCooldown = pr.soundCooldown
				m.PlaySound("pushed_by_player")
			}
			m.SetVelocity(m.Velocity().Add(kick))
			m.SyncMovement()
			break
		}
	}
	p.last, p.next = seen, p.last
}

// applies reports whether the preset's filters pass for the pusher e.
func (pr pushPreset) applies(m *mob.Mob, e world.Entity) bool {
	if pr.filter == nil {
		return true
	}
	ctx := m.Ctx()
	ctx.Other = e
	return pr.filter.Test(ctx)
}
