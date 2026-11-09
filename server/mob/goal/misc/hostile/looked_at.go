//go:build crossfly_full

package hostile

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// LookedAt is minecraft:looked_at (1.26; enderman stares, creaking
// freezes; not in the 1.21.3 dump, which hard-codes the enderman's
// EnderMan::isLookingAtMe; source: the 1.26 documentation and java/paper
// EnderMan.isBeingStaredBy).
//
//	search_radius (10), find_players_only (false), filters (the looker as
//	"other"), field_of_view (26 degrees), scale_fov_by_distance (true: the
//	allowed angle shrinks with distance, Java's 1 - 0.025/d for 26),
//	look_at_locations ([head]; head, body, feet + vertical_offset),
//	line_of_sight_obstruction_type (blocks block the view),
//	min_looked_at_duration (0 s), looked_at_cooldown (0 s),
//	set_target ("once_and_stop_scanning", "once_and_keep_scanning", "never"),
//	looked_at_event / not_looked_at_event.
//
// Every scan (each tick after the cooldown) it finds the nearest entity
// looking at one of the locations. Looked at for min_looked_at_duration it
// fires looked_at_event once and, unless set_target is "never", makes the
// looker the target; once_and_stop_scanning stops scanning while the mob
// has a target. When nobody looks any more not_looked_at_event fires.
type LookedAt struct {
	radius      float64
	playersOnly bool
	filter      mob.Filter
	fov         float64
	scale       bool
	locations   []lookLocation
	minDuration int
	cooldown    int
	setTarget   string
	onLooked    kit.Trigger
	onNotLooked kit.Trigger

	lookedFor int
	looked    bool
	next      int64
}

type lookLocation struct {
	where  string
	offset float64
}

func newLookedAt(key string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		Radius      *float64        `json:"search_radius"`
		PlayersOnly bool            `json:"find_players_only"`
		Filters     json.RawMessage `json:"filters"`
		FOV         *float64        `json:"field_of_view"`
		Scale       *bool           `json:"scale_fov_by_distance"`
		Locations   []struct {
			Location string  `json:"location"`
			Offset   float64 `json:"vertical_offset"`
		} `json:"look_at_locations"`
		MinDuration float64         `json:"min_looked_at_duration"`
		Cooldown    float64         `json:"looked_at_cooldown"`
		SetTarget   *string         `json:"set_target"`
		Looked      json.RawMessage `json:"looked_at_event"`
		NotLooked   json.RawMessage `json:"not_looked_at_event"`
	}
	if !kit.IsNull(raw) {
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("%v: %w", key, err)
		}
	}
	c := &LookedAt{
		radius: kit.Or(v.Radius, 10), playersOnly: v.PlayersOnly, fov: kit.Or(v.FOV, 26), scale: kit.Or(v.Scale, true),
		minDuration: kit.Ticks(v.MinDuration), cooldown: kit.Ticks(v.Cooldown), setTarget: kit.Or(v.SetTarget, "once_and_stop_scanning"),
	}
	for _, l := range v.Locations {
		c.locations = append(c.locations, lookLocation{l.Location, l.Offset})
	}
	if len(c.locations) == 0 {
		c.locations = []lookLocation{{"head", 0}}
	}
	var err error
	if c.filter, err = kit.Filter(v.Filters); err != nil {
		return nil, err
	}
	if c.onLooked, err = kit.ParseTrigger(v.Looked); err != nil {
		return nil, err
	}
	if c.onNotLooked, err = kit.ParseTrigger(v.NotLooked); err != nil {
		return nil, err
	}
	return c, nil
}

// Attach ...
func (*LookedAt) Attach(*mob.Mob) {}

// Detach ...
func (*LookedAt) Detach(*mob.Mob) {}

// LookedAt reports whether someone looks at the mob.
func (c *LookedAt) LookedAt() bool { return c.looked }

func (c *LookedAt) point(m *mob.Mob, l lookLocation) mgl64.Vec3 {
	p := m.Position()
	switch l.where {
	case "body":
		p = sense.Center(m)
	case "feet":
	default:
		p = m.EyePosition()
	}
	return p.Add(mgl64.Vec3{0, l.offset, 0})
}

// viewOf returns the eye position and look direction of e.
func viewOf(e world.Entity) (mgl64.Vec3, mgl64.Vec3, bool) {
	switch e := e.(type) {
	case *player.Player:
		return entity.EyePosition(e), e.Rotation().Vec3(), true
	}
	if m, ok := mob.Of(e); ok {
		return m.EyePosition(), mgl64.Vec3{-math.Sin(mgl64.DegToRad(m.HeadYaw())) * math.Cos(mgl64.DegToRad(m.Pitch())), -math.Sin(mgl64.DegToRad(m.Pitch())), math.Cos(mgl64.DegToRad(m.HeadYaw())) * math.Cos(mgl64.DegToRad(m.Pitch()))}, true
	}
	return mgl64.Vec3{}, mgl64.Vec3{}, false
}

// IsLookingAt reports whether e looks at one of m's locations (EnderMan
// isBeingStaredBy generalised to field_of_view).
func (c *LookedAt) IsLookingAt(m *mob.Mob, e world.Entity) bool {
	eye, dir, ok := viewOf(e)
	if !ok || dir.Len() == 0 {
		return false
	}
	dir = dir.Normalize()
	half := mgl64.DegToRad(c.fov / 2)
	for _, l := range c.locations {
		to := c.point(m, l).Sub(eye)
		d := to.Len()
		if d < 1e-4 {
			continue
		}
		limit := math.Cos(half)
		if c.scale {
			limit = 1 - (1-math.Cos(half))/math.Max(d, 1)
		}
		if dir.Dot(to.Mul(1/d)) <= limit {
			continue
		}
		if sense.LineOfSight(m.Tx(), eye, c.point(m, l)) {
			return true
		}
	}
	return false
}

func (c *LookedAt) tick(m *mob.Mob) {
	if m.Dead() || m.Ticks() < c.next {
		return
	}
	c.next = m.Ticks() + int64(max(c.cooldown, 1))
	_, hasTarget := m.Target()
	if hasTarget && c.setTarget == "once_and_stop_scanning" && c.looked {
		return
	}
	var looker world.Entity
	for _, e := range kit.Around(m, c.radius, c.radius) {
		if !kit.Alive(e) || kit.DistSq(e.Position(), m.Position()) > c.radius*c.radius {
			continue
		}
		if p, ok := e.(*player.Player); ok {
			if !p.GameMode().Visible() {
				continue
			}
		} else if c.playersOnly {
			continue
		}
		if !c.filter.Test(kit.OtherCtx(m, e)) || !c.IsLookingAt(m, e) {
			continue
		}
		looker = e
		break
	}
	if looker == nil {
		c.lookedFor = 0
		if c.looked {
			c.looked = false
			c.onNotLooked.Fire(m, nil)
		}
		return
	}
	c.lookedFor += max(c.cooldown, 1)
	if c.looked || c.lookedFor < c.minDuration {
		return
	}
	c.looked = true
	if c.setTarget != "never" && !hasTarget && !kit.Creative(looker) {
		m.SetTarget(looker)
	}
	c.onLooked.Fire(m, kit.OtherCtx(m, looker))
}
