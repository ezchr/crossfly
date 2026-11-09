//go:build crossfly_full

package misc

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
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
	// colour is particle_color (ARGB): the colour of the spell particles
	// while casting (actor data DataSpellCastingColor).
	colour    int32
	hasColour bool
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
	ParticleColor   string          `json:"particle_color"`
}

func (r rawSpellChoice) parse(defMax float64) (*spellChoice, error) {
	f, err := kit.Filter(r.Filters)
	if err != nil {
		return nil, err
	}
	c := &spellChoice{
		weight: kit.Or(r.Weight, 1), minR: r.MinRange, maxR: kit.Or(r.MaxRange, defMax),
		cooldown: kit.Ticks(r.CooldownTime), filter: f, startSound: r.StartSoundEvent,
		casting: kit.Or(r.DoCasting, true), cast: kit.Ticks(kit.Or(r.CastDuration, 0)),
	}
	c.colour, c.hasColour = parseColour(r.ParticleColor)
	return c, nil
}

// parseColour reads a colour written "#AARRGGBB" (or "#RRGGBB", opaque) as
// an ARGB integer.
func parseColour(s string) (int32, bool) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 && len(s) != 8 {
		return 0, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, false
	}
	if len(s) == 6 {
		v |= 0xff000000
	}
	return int32(uint32(v)), true
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
	if g.cur.hasColour {
		// SendEventGoal::start shows the spell colour, as summons do.
		m.SetActorData(protocol.EntityDataKeyDataSpellCastingColor, g.cur.colour)
	}
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
// BDS SummonActorGoal with SummonSpellData / SummonSpellStage): with a
// target, a spell is picked (_selectBestSpell) and cast once the goal's one
// cooldown is over. The spell's stages run one after another, each
// base_delay ticks after the previous one ended (the first after the
// start), summoning num_entities_spawned entities delay_per_summon apart in
// a "circle" of radius size around, or a "line" of length size towards the
// other one from, the caster ("self") or the target ("target"), placed on
// where the two stood when the cast started. The cast (cast_duration, the
// casting flag and the spell particle colour on) keeps the caster still and
// facing the target; the stages may outlast it. When the goal stops, the
// cooldown of the spell cast starts.
type summonEntity struct {
	spellBase
	full mob.ControlFlags
	seqs map[*spellChoice][]summonStep

	castLeft                int
	stage, count, stageTick int
	cooldownEnd             int64
	targetStart, selfStart  mgl64.Vec3
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
	// lifespan is entity_lifespan in ticks, or 0 for summons that live on.
	lifespan int
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
				Lifespan       float64  `json:"entity_lifespan"`
			} `json:"sequence"`
		} `json:"summon_choices"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	full := spec.FlagsOr(mob.FlagMove | mob.FlagLook)
	g := &summonEntity{spellBase: spellBase{GoalBase: mob.GoalBase{Control: full}}, full: full, seqs: map[*spellChoice][]summonStep{}}
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
				lifespan: kit.Ticks(s.Lifespan),
			})
		}
		g.choices = append(g.choices, c)
	}
	return g, nil
}

// CanUse is SummonActorGoal::canUse: a target, a spell to cast and the
// cooldown over.
func (g *summonEntity) CanUse(m *mob.Mob) bool {
	t, ok := m.Target()
	if !ok || m.Ticks() < g.cooldownEnd {
		return false
	}
	g.cur = g.selectBestSpell(m, t)
	return g.cur != nil
}

// selectBestSpell is SummonActorGoal::_selectBestSpell: a spell is a
// candidate when the target is within its activation range, none of its
// stages has summon_cap entities of its type around the caster (in its box
// grown by summon_cap_radius) and it passes its filters; one is picked by
// weight.
func (g *summonEntity) selectBestSpell(m *mob.Mob, t world.Entity) *spellChoice {
	d := kit.DistSq(m.Position(), t.Position())
	total := 0
	var cands []*spellChoice
	for _, c := range g.choices {
		if d > c.maxR*c.maxR || d < c.minR*c.minR || g.atCap(m, c) || !c.filter.Test(kit.OtherCtx(m, t)) {
			continue
		}
		cands = append(cands, c)
		total += c.weight
	}
	if total <= 0 {
		return nil
	}
	n := m.Rand().IntN(total)
	for _, c := range cands {
		if n < c.weight {
			return c
		}
		n -= c.weight
	}
	return nil
}

func (g *summonEntity) atCap(m *mob.Mob, c *spellChoice) bool {
	for _, s := range g.seqs[c] {
		if s.cap <= 0 || s.entity == "" {
			continue
		}
		n := 0
		for e := range m.Tx().EntitiesWithin(m.BBox().Grow(s.capRadius)) {
			if e.H().Type().EncodeEntity() == s.entity && kit.Alive(e) {
				n++
			}
		}
		if n >= s.cap {
			return true
		}
	}
	return false
}

// CanContinueToUse is SummonActorGoal::canContinueToUse: a target, and the
// cast or a stage left.
func (g *summonEntity) CanContinueToUse(m *mob.Mob) bool {
	_, ok := m.Target()
	return ok && g.cur != nil && (g.castLeft > 0 || g.stage < len(g.seqs[g.cur]))
}

// Start is SummonActorGoal::start: where the target and the caster stand
// is kept for the whole cast; the start sound and the first stage's sound
// play, the cast begins (casting flag, particle colour) and the caster
// stops moving. The summon counter is not reset (a cast cut short in the
// middle of a stage leaves its count to the next one, as in BDS).
func (g *summonEntity) Start(m *mob.Mob) {
	t, ok := m.Target()
	if !ok {
		return
	}
	g.stageTick, g.stage = -1, 0
	g.targetStart, g.selfStart = t.Position(), m.Position()
	kit.Sound(m, g.cur.startSound)
	g.castLeft = g.cur.cast
	if g.cur.hasColour {
		m.SetActorData(protocol.EntityDataKeyDataSpellCastingColor, g.cur.colour)
	}
	if g.cur.casting {
		kit.Flag(m, protocol.EntityDataFlagCasting, g.castLeft > 0)
	}
	if steps := g.seqs[g.cur]; len(steps) > 0 {
		kit.Sound(m, steps[0].sound)
	}
	g.Control = 0
	if g.castLeft > 0 {
		g.Control = g.full
		kit.Stop(m)
	}
}

// Tick is SummonActorGoal::tick.
func (g *summonEntity) Tick(m *mob.Mob) {
	t, ok := m.Target()
	if !ok {
		return
	}
	m.LookControl().SetLookAtEntity(t, 30, 30)
	if g.castLeft--; g.castLeft == 0 {
		g.Control = 0
		kit.Flag(m, protocol.EntityDataFlagCasting, false)
	}
	steps := g.seqs[g.cur]
	if g.stage >= len(steps) {
		return
	}
	s := &steps[g.stage]
	if g.stageTick++; g.stageTick >= s.at {
		for g.count < s.count {
			g.summonOne(m, t, s)
			g.stageTick -= s.per
			g.count++
			if g.stageTick < s.at {
				break
			}
		}
	}
	if g.count >= s.count {
		g.count, g.stageTick = 0, 0
		g.stage++
		kit.Sound(m, s.sound)
	}
}

// summonOne places the next summon of stage s: in a line, the count+1-th
// of count steps of size/count towards the other one; in a circle, at an
// angle of 2*pi*count/n past the direction to the other one. Heights run
// from the lower of the target and the caster to one above the higher one.
func (g *summonEntity) summonOne(m *mob.Mob, t world.Entity, s *summonStep) {
	origin, other := g.selfStart, g.targetStart
	if s.fromTarget {
		origin, other = g.targetStart, g.selfStart
	}
	minY := math.Min(t.Position()[1], m.Position()[1])
	maxY := math.Max(t.Position()[1], m.Position()[1]) + 1
	angle := math.Atan2(other[2]-origin[2], other[0]-origin[0])
	var x, z float64
	switch s.shape {
	case "line":
		dir := mgl64.Vec3{other[0] - origin[0], 0, other[2] - origin[2]}
		if dir.Len() >= 1e-4 {
			dir = dir.Normalize()
		} else {
			dir = mgl64.Vec3{}
		}
		step := s.size / float64(max(s.count, 1)) * float64(g.count+1)
		x, z = origin[0]+dir[0]*step, origin[2]+dir[2]*step
	default: // circle
		a := 2*float64(g.count)*math.Pi/float64(max(s.count, 1)) + angle
		x, z = origin[0]+math.Cos(a)*s.size, origin[2]+math.Sin(a)*s.size
	}
	createSpellEntity(m, x, z, minY, maxY, s)
}

// createSpellEntity is SummonActorGoal::_createSpellEntity: from maxY down
// to one below minY, the first block that does not hold up and stands on
// one that does; the summon goes on the block's corner (BDS spawns at the
// block position itself), raised to the top of what collision the block
// has, facing south. It is owned by the caster and lives entity_lifespan;
// a mob gets the summon event.
func createSpellEntity(m *mob.Mob, x, z, minY, maxY float64, s *summonStep) {
	tx := m.Tx()
	p := cube.Pos{int(math.Floor(x)), int(math.Floor(maxY)), int(math.Floor(z))}
	bottom := int(math.Floor(minY)) - 1
	for kit.Solid(tx, p) || !kit.Solid(tx, p.Side(cube.FaceDown)) {
		if p[1]--; p[1] < bottom || p[1] <= tx.Range()[0] {
			return
		}
	}
	yOff := 0.0
	for _, b := range tx.Block(p).Model().BBox(p, tx) {
		yOff = math.Max(yOff, b.Max()[1])
	}
	pos := mgl64.Vec3{float64(p[0]), float64(p[1]) + yOff, float64(p[2])}
	if s.entity == FangID {
		SpawnFang(tx, pos, m, s.lifespan)
		return
	}
	opts := mob.SpawnOptions{Event: s.event, Configure: func(c *mob.Mob) {
		c.UpdateMeta(func(meta *mob.Meta) { meta.Owner = m.H() })
	}}
	// The summon is not given the caster's target: a vex copies it itself
	// (vex_copy_owner_target).
	_, _ = mob.Spawn(tx, s.entity, pos, opts)
}

// Stop is SummonActorGoal::stop: the cast ends and the spell's cooldown
// starts.
func (g *summonEntity) Stop(m *mob.Mob) {
	g.Control = g.full
	kit.Flag(m, protocol.EntityDataFlagCasting, false)
	if g.cur != nil {
		g.cooldownEnd = m.Ticks() + int64(g.cur.cooldown)
	}
	g.cur, g.subject = nil, nil
}

// AppendDebugInfo ...
func (g *summonEntity) AppendDebugInfo(b *strings.Builder) { b.WriteString("SummonEntity") }
