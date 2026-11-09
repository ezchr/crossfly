//go:build crossfly_full

package misc

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// spellBase is what send_event and summon_entity share (BDS SendEventGoal,
// SummonEntityGoal / SummonSpellData): weighted choices with a cooldown each,
// an activation range, a cast of cast_duration seconds with the casting
// actor flag on while the mob stands and faces its subject, and a timed
// sequence of steps.
type spellBase struct {
	mob.GoalBase
	choices []*spellChoice
	cur     *spellChoice
	subject *world.EntityHandle
	t       int // ticks into the cast
}

type spellChoice struct {
	weight         int
	minR, maxR     float64
	cooldown, cast int
	filter         mob.Filter
	startSound     string
	casting        bool
	next           int64
}

type rawSpellChoice struct {
	Weight          *int            `json:"weight"`
	MinRange        float64         `json:"min_activation_range"`
	MaxRange        *float64        `json:"max_activation_range"`
	CooldownTime    float64         `json:"cooldown_time"`
	CastDuration    *float64        `json:"cast_duration"`
	Filters         json.RawMessage `json:"filters"`
	StartSoundEvent string          `json:"start_sound_event"`
	DoCasting       *bool           `json:"do_casting"`
}

func (r rawSpellChoice) parse(defMax float64) (*spellChoice, error) {
	f, err := kit.Filter(r.Filters)
	if err != nil {
		return nil, err
	}
	return &spellChoice{
		weight: kit.Or(r.Weight, 1), minR: r.MinRange, maxR: kit.Or(r.MaxRange, defMax),
		cooldown: kit.Ticks(r.CooldownTime), filter: f, startSound: r.StartSoundEvent,
		casting: kit.Or(r.DoCasting, true), cast: kit.Ticks(kit.Or(r.CastDuration, 0)),
	}, nil
}

// pick chooses a ready choice by weight whose subject passes.
func (g *spellBase) pick(m *mob.Mob, subjectFor func(c *spellChoice) (world.Entity, bool)) bool {
	type opt struct {
		c *spellChoice
		e world.Entity
	}
	var opts []opt
	total := 0
	for _, c := range g.choices {
		if m.Ticks() < c.next || c.weight <= 0 {
			continue
		}
		if e, ok := subjectFor(c); ok {
			opts = append(opts, opt{c, e})
			total += c.weight
		}
	}
	if total == 0 {
		return false
	}
	n := m.Rand().IntN(total)
	for _, o := range opts {
		if n -= o.c.weight; n < 0 {
			g.cur, g.subject = o.c, nil
			if o.e != nil {
				g.subject = o.e.H()
			}
			return true
		}
	}
	return false
}

func inRange(m *mob.Mob, e world.Entity, c *spellChoice) bool {
	d := math.Sqrt(kit.DistSq(m.Position(), e.Position()))
	return d >= c.minR && d <= c.maxR
}

func (g *spellBase) start(m *mob.Mob) {
	g.t = 0
	kit.Stop(m)
	if g.cur.casting {
		kit.Flag(m, protocol.EntityDataFlagCasting, true)
	}
	kit.Sound(m, g.cur.startSound)
}

func (g *spellBase) tickFace(m *mob.Mob) {
	g.t++
	if g.subject == nil {
		return
	}
	if e, ok := g.subject.Entity(m.Tx()); ok {
		kit.LookAtEntity(m, e)
	}
}

func (g *spellBase) stop(m *mob.Mob) {
	if g.cur != nil {
		g.cur.next = m.Ticks() + int64(g.cur.cooldown)
	}
	kit.Flag(m, protocol.EntityDataFlagCasting, false)
	g.cur, g.subject = nil, nil
}

// sendEvent is minecraft:behavior.send_event (evoker "wololo", BDS
// SendEventGoal): the subject is the nearest entity within
// max_activation_range (16) and beyond min_activation_range (0) that
// passes the choice's filters (as "other"); during the cast each sequence
// step fires its event on the subject base_delay seconds in, with its
// sound_event.
type sendEvent struct {
	spellBase
	seqs map[*spellChoice][]sendStep
}

type sendStep struct {
	at    int
	event string
	sound string
}

func newSendEvent(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Choices []struct {
			rawSpellChoice
			Sequence []struct {
				BaseDelay  float64 `json:"base_delay"`
				Event      string  `json:"event"`
				SoundEvent string  `json:"sound_event"`
			} `json:"sequence"`
		} `json:"event_choices"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	g := &sendEvent{spellBase: spellBase{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}}, seqs: map[*spellChoice][]sendStep{}}
	for _, rc := range v.Choices {
		c, err := rc.parse(16)
		if err != nil {
			return nil, err
		}
		for _, s := range rc.Sequence {
			g.seqs[c] = append(g.seqs[c], sendStep{at: kit.Ticks(s.BaseDelay), event: s.Event, sound: s.SoundEvent})
		}
		g.choices = append(g.choices, c)
	}
	return g, nil
}

// CanUse ...
func (g *sendEvent) CanUse(m *mob.Mob) bool {
	return g.pick(m, func(c *spellChoice) (world.Entity, bool) {
		return kit.Nearest(m, c.maxR, c.maxR, func(e world.Entity) bool {
			return inRange(m, e, c) && c.filter.Test(kit.OtherCtx(m, e))
		})
	})
}

// CanContinueToUse ...
func (g *sendEvent) CanContinueToUse(m *mob.Mob) bool {
	if g.cur == nil || g.t > g.cur.cast {
		return false
	}
	e, ok := g.subject.Entity(m.Tx())
	return ok && kit.Alive(e)
}

// Start ...
func (g *sendEvent) Start(m *mob.Mob) { g.start(m) }

// Tick ...
func (g *sendEvent) Tick(m *mob.Mob) {
	g.tickFace(m)
	e, ok := g.subject.Entity(m.Tx())
	if !ok {
		return
	}
	for _, s := range g.seqs[g.cur] {
		if s.at != g.t-1 {
			continue
		}
		kit.Sound(m, s.sound)
		if om, ok := kit.Mob(e); ok && s.event != "" {
			ctx := om.Ctx()
			ctx.Other = m
			om.Fire(s.event, ctx)
		}
	}
}

// Stop ...
func (g *sendEvent) Stop(m *mob.Mob) { g.stop(m) }

// AppendDebugInfo ...
func (g *sendEvent) AppendDebugInfo(b *strings.Builder) { b.WriteString("SendEvent") }

// summonEntity is minecraft:behavior.summon_entity (evoker fangs and vexes,
// BDS SummonEntityGoal / SummonSpellStage): the subject is the mob's
// target, within the choice's activation range (default 0..32) and passing
// its filters. Each sequence step summons num_entities_spawned (1)
// entity_type base_delay seconds into the cast, delay_per_summon apart, in
// a "circle" of radius size around, or a "line" of length size towards the
// target from, the mob (target "self") or the target (target "target");
// summon_cap / summon_cap_radius limit live summons of the type, and each
// mob summoned gets summon_event (entity_spawned) and the caster as owner.
//
// minecraft:evocation_fang is not a mob Dragonfly has, so a fang is its
// bite: 6 damage to living entities (not the caster or its family) in the
// fang's box 8 ticks after it would appear (source: java/paper EvokerFangs)
// with no fang shown.
type summonEntity struct {
	spellBase
	seqs    map[*spellChoice][]summonStep
	pending []pendingSummon
}

type summonStep struct {
	at, per       int
	count         int
	entity, shape string
	size          float64
	sound, event  string
	cap           int
	capRadius     float64
	fromTarget    bool
}

type pendingSummon struct {
	at   int
	pos  mgl64.Vec3
	yaw  float64
	step *summonStep
	bite bool
}

func newSummonEntity(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Choices []struct {
			rawSpellChoice
			Sequence []struct {
				BaseDelay      float64  `json:"base_delay"`
				DelayPerSummon float64  `json:"delay_per_summon"`
				Num            *int     `json:"num_entities_spawned"`
				EntityType     string   `json:"entity_type"`
				Shape          string   `json:"shape"`
				Size           *float64 `json:"size"`
				SoundEvent     string   `json:"sound_event"`
				SummonEvent    string   `json:"summon_event"`
				SummonCap      int      `json:"summon_cap"`
				CapRadius      float64  `json:"summon_cap_radius"`
				Target         string   `json:"target"`
			} `json:"sequence"`
		} `json:"summon_choices"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	g := &summonEntity{spellBase: spellBase{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}}, seqs: map[*spellChoice][]summonStep{}}
	for _, rc := range v.Choices {
		c, err := rc.parse(32)
		if err != nil {
			return nil, err
		}
		for _, s := range rc.Sequence {
			g.seqs[c] = append(g.seqs[c], summonStep{
				at: kit.Ticks(s.BaseDelay), per: kit.Ticks(s.DelayPerSummon), count: kit.Or(s.Num, 1),
				entity: s.EntityType, shape: s.Shape, size: kit.Or(s.Size, 1), sound: s.SoundEvent,
				event: s.SummonEvent, cap: s.SummonCap, capRadius: s.CapRadius, fromTarget: s.Target == "target",
			})
		}
		g.choices = append(g.choices, c)
	}
	return g, nil
}

// CanUse ...
func (g *summonEntity) CanUse(m *mob.Mob) bool {
	t, ok := m.Target()
	if !ok || !kit.Alive(t) {
		return false
	}
	return g.pick(m, func(c *spellChoice) (world.Entity, bool) {
		return t, inRange(m, t, c) && c.filter.Test(kit.OtherCtx(m, t))
	})
}

// CanContinueToUse ...
func (g *summonEntity) CanContinueToUse(m *mob.Mob) bool {
	return g.cur != nil && (g.t <= g.cur.cast || len(g.pending) > 0)
}

// Start ...
func (g *summonEntity) Start(m *mob.Mob) {
	g.start(m)
	g.pending = nil
}

// Tick ...
func (g *summonEntity) Tick(m *mob.Mob) {
	g.tickFace(m)
	steps := g.seqs[g.cur]
	for i := range steps {
		if steps[i].at == g.t-1 {
			g.plan(m, &steps[i])
		}
	}
	due := g.pending
	g.pending = nil
	for _, p := range due {
		if p.at > g.t {
			g.pending = append(g.pending, p)
			continue
		}
		g.summon(m, p) // may queue a fang bite into g.pending
	}
}

// plan lays out a step's summons (SummonSpellStage shapes).
func (g *summonEntity) plan(m *mob.Mob, s *summonStep) {
	kit.Sound(m, s.sound)
	origin := m.Position()
	var target mgl64.Vec3
	if t, ok := m.Target(); ok {
		target = t.Position()
		if s.fromTarget {
			origin = target
		}
	}
	yaw := math.Atan2(target[2]-m.Position()[2], target[0]-m.Position()[0])
	for i := 0; i < s.count; i++ {
		var pos mgl64.Vec3
		switch s.shape {
		case "line":
			d := s.size / float64(max(s.count, 1)) * float64(i+1)
			pos = origin.Add(mgl64.Vec3{math.Cos(yaw) * d, 0, math.Sin(yaw) * d})
		default: // circle
			a := yaw + float64(i)*2*math.Pi/float64(max(s.count, 1))
			pos = origin.Add(mgl64.Vec3{math.Cos(a) * s.size, 0, math.Sin(a) * s.size})
		}
		g.pending = append(g.pending, pendingSummon{at: g.t + i*s.per, pos: pos, yaw: mgl64.RadToDeg(yaw) - 90, step: s})
	}
}

func (g *summonEntity) summon(m *mob.Mob, p pendingSummon) {
	s := p.step
	tx := m.Tx()
	if p.bite {
		fangBite(m, p.pos)
		return
	}
	// Ground under the spot, within 2 blocks either way.
	if gp, ok := kit.Ground(tx, cube.PosFromVec3(p.pos), 2); ok {
		p.pos[1] = float64(gp[1])
	}
	if s.entity == "minecraft:evocation_fang" {
		if _, ok := mob.TypeFor(s.entity); !ok {
			g.pending = append(g.pending, pendingSummon{at: g.t + 8, pos: p.pos, step: s, bite: true})
			return
		}
	}
	if s.cap > 0 {
		n := 0
		for _, e := range kit.Around(m, s.capRadius, s.capRadius) {
			if om, ok := kit.Mob(e); ok && om.Identifier() == s.entity && !om.Dead() {
				n++
			}
		}
		if n >= s.cap {
			return
		}
	}
	opts := mob.SpawnOptions{Event: s.event, Configure: func(c *mob.Mob) {
		c.UpdateMeta(func(meta *mob.Meta) { meta.Owner = m.H() })
	}}
	opts.Rotation = cube.Rotation{p.yaw, 0}
	if c, err := mob.Spawn(tx, s.entity, p.pos, opts); err == nil {
		if t, ok := m.Target(); ok {
			c.SetTarget(t)
		}
	}
}

// fangBite is an evoker fang's bite (source: java/paper EvokerFangs.dealDamageTo):
// 6 magic damage to living entities in the 0.5 x 0.8 box, sparing the caster
// and mobs of its family (illagers).
func fangBite(m *mob.Mob, pos mgl64.Vec3) {
	box := cube.Box(pos[0]-0.25, pos[1], pos[2]-0.25, pos[0]+0.25, pos[1]+0.8, pos[2]+0.25)
	for e := range m.Tx().EntitiesWithin(box.Grow(2)) {
		if e.H() == m.H() || !kit.Alive(e) || !kit.BBox(e).IntersectsWith(box) {
			continue
		}
		if om, ok := kit.Mob(e); ok && om.HasFamily("illager") {
			continue
		}
		if l, ok := e.(entity.Living); ok {
			l.Hurt(6, entity.AttackDamageSource{Attacker: m})
		}
	}
	m.Tx().PlaySound(pos, mobSound("fang", "minecraft:evocation_fang"))
}

func mobSound(event, id string) world.Sound { return mob.Sound{Event: event, Identifier: id} }

// Stop ...
func (g *summonEntity) Stop(m *mob.Mob) {
	g.stop(m)
	g.pending = nil
}

// AppendDebugInfo ...
func (g *summonEntity) AppendDebugInfo(b *strings.Builder) { b.WriteString("SummonEntity") }
