//go:build crossfly_full

package jraider

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/goal/misc"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/mob/jraid"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

func init() {
	javamob.Register(javamob.Type{
		ID: EvokerID, Category: javamob.CategoryMonster, Width: 0.6, Height: 1.95, EyeHeight: 1.62,
		NotInPeaceful: true, Illager: true, XPReward: 10, LootTable: "entities/evoker",
		Families:   []string{"evocation_illager", "illager", "monster", "mob"},
		Attributes: attributes("minecraft:evoker"),
		New:        func() javamob.Behaviour { return &Evoker{base: newBase(jraid.Evoker, "celebrate")} },
	})
}

// spellColour is the casting colour the clients read (actor data SpellCastingColor, ARGB): javasession maps the
// three evoker colours to the Java spell ids.
var spellColour = map[jraid.Spell]int32{
	jraid.SpellSummonVex: int32(-0x4c4c34), // #FFB3B3CC
	jraid.SpellFangs:     int32(-0x99b2a7), // #FF664D59
	jraid.SpellWololo:    int32(-0x4c7fcd), // #FFB38033
}

// Evoker is the Evoker class (SpellcasterIllager).
type Evoker struct {
	base
	caster jraid.Spellcaster
	wololo *world.EntityHandle
	fangs  []pendingFang
}

type pendingFang struct {
	pos   mgl64.Vec3
	delay int
}

// RegisterGoals is Evoker.registerGoals.
func (e *Evoker) RegisterGoals(m *javamob.Mob) {
	e.registerRaiderGoals(m)
	m.Goals().Add(0, goal.NewFloat(m))
	m.Goals().Add(1, &castingSpell{GoalBase: javamob.GoalBase{Control: javamob.FlagMove | javamob.FlagLook}, e: e})
	m.Goals().Add(2, goal.NewAvoidEntity(isPlayer, 8, 0.6, 1))
	m.Goals().Add(3, goal.NewAvoidEntity(goal.IsType("minecraft:creaking"), 8, 0.6, 1))
	m.Goals().Add(4, &useSpell{e: e, s: jraid.UseSpell{Spell: jraid.SpellSummonVex}, cast: e.summonVexes, extra: e.canSummon, sound: "prepare.summon"})
	m.Goals().Add(5, &useSpell{e: e, s: jraid.UseSpell{Spell: jraid.SpellFangs}, cast: e.fangAttack, sound: "prepare.attack"})
	m.Goals().Add(6, &useSpell{e: e, s: jraid.UseSpell{Spell: jraid.SpellWololo}, cast: e.wololoCast, sound: "prepare.wololo", wololo: true})
	m.Goals().Add(8, goal.NewRandomStroll(0.6))
	m.Goals().Add(9, goal.NewLookAtPlayer(3))
	m.Goals().Add(10, lookAtMobs(8))
	m.Targets().Add(1, hurtByTarget().SetAlertOthers())
	players := goal.NewNearestAttackableTarget(nil, true, false)
	players.UnseenMemoryTicks = 300
	m.Targets().Add(2, players)
	villagers := villagerTarget(false)
	villagers.UnseenMemoryTicks = 300
	m.Targets().Add(3, villagers)
	m.Targets().Add(3, golemTarget(false))
}

// CanAttack is AbstractIllager.canAttack.
func (e *Evoker) CanAttack(_ *javamob.Mob, t world.Entity) bool { return illagerCanAttack(t) }

// FinalizeSpawn: evokers have no equipment.
func (e *Evoker) FinalizeSpawn(m *javamob.Mob, reason javamob.SpawnReason) {
	e.finalizeRaider(m, reason)
}

// ApplyRaidBuffs is Evoker.applyRaidBuffs: nothing.
func (e *Evoker) ApplyRaidBuffs(*javamob.Mob, int) {}

// Tick raises the fangs whose warm-up ran out.
func (e *Evoker) Tick(m *javamob.Mob) {
	e.base.Tick(m)
	if len(e.fangs) == 0 {
		return
	}
	rest := e.fangs[:0]
	for _, f := range e.fangs {
		if f.delay--; f.delay > 0 {
			rest = append(rest, f)
			continue
		}
		misc.SpawnFang(m.Tx(), f.pos, m, 0)
	}
	e.fangs = rest
}

// CustomServerAiStep is SpellcasterIllager.customServerAiStep: the casting time runs down.
func (e *Evoker) CustomServerAiStep(m *javamob.Mob) {
	e.base.CustomServerAiStep(m)
	e.caster.ServerAIStep()
}

// showSpell is setIsCastingSpell: the clients raise the arms and show the spell's particles.
func (e *Evoker) showSpell(m *javamob.Mob, s jraid.Spell) {
	e.caster.Current = s
	if c, ok := spellColour[s]; ok {
		m.SetActorData(protocol.EntityDataKeyDataSpellCastingColor, c)
	}
	m.SetActorFlag(protocol.EntityDataFlagCasting, s != jraid.SpellNone)
}

func (e *Evoker) EncodeNBT(data map[string]any) {
	e.base.EncodeNBT(data)
	data["SpellTicks"] = int32(e.caster.CastingTicks)
}

func (e *Evoker) DecodeNBT(m *javamob.Mob, data map[string]any) {
	e.base.DecodeNBT(m, data)
	if v, ok := data["SpellTicks"].(int32); ok {
		e.caster.CastingTicks = int(v)
	}
}

// castingSpell is Evoker.EvokerCastingSpellGoal: while casting, stand still and face the target (or the sheep).
type castingSpell struct {
	javamob.GoalBase
	e *Evoker
}

func (g *castingSpell) CanUse(*javamob.Mob) bool { return g.e.caster.CastingTicks > 0 }
func (g *castingSpell) Start(m *javamob.Mob)     { m.Navigation().Stop() }
func (g *castingSpell) Stop(m *javamob.Mob)      { g.e.showSpell(m, jraid.SpellNone) }
func (g *castingSpell) Tick(m *javamob.Mob) {
	if t, ok := m.Target(); ok {
		m.LookControl().SetLookAtEntity(t, m.MaxHeadYRot(), 40)
		return
	}
	if g.e.wololo != nil {
		if s, ok := g.e.wololo.Entity(m.Tx()); ok {
			m.LookControl().SetLookAtEntity(s, m.MaxHeadYRot(), 40)
		}
	}
}

// useSpell is a SpellcasterUseSpellGoal of the evoker.
type useSpell struct {
	javamob.GoalBase
	e      *Evoker
	s      jraid.UseSpell
	cast   func(m *javamob.Mob)
	extra  func(m *javamob.Mob) bool
	sound  string
	wololo bool
}

func liveTarget(m *javamob.Mob) bool {
	t, ok := m.Target()
	if !ok {
		return false
	}
	return alive(t)
}

func (g *useSpell) CanUse(m *javamob.Mob) bool {
	if g.wololo {
		return g.canWololo(m)
	}
	if !g.s.CanUse(&g.e.caster, liveTarget(m), m.TickCount()) {
		return false
	}
	return g.extra == nil || g.extra(m)
}

func (g *useSpell) CanContinueToUse(m *javamob.Mob) bool {
	if g.wololo {
		return g.e.wololo != nil && g.s.WarmupDelay > 0
	}
	return liveTarget(m) && g.s.WarmupDelay > 0
}

func (g *useSpell) Start(m *javamob.Mob) {
	g.s.Start(&g.e.caster, m.TickCount(), func(t int) int { return javamob.AdjustedTickDelay(g, t) })
	m.PlaySound(g.sound)
	g.e.showSpell(m, g.s.Spell)
}

func (g *useSpell) Stop(*javamob.Mob) {
	if g.wololo {
		g.e.wololo = nil
	}
}

func (g *useSpell) Tick(m *javamob.Mob) {
	if g.s.Tick() {
		g.cast(m)
		m.PlaySound("cast.spell")
	}
}

// canWololo is EvokerWololoSpellGoal.canUse: no target, not casting, the interval passed, mob griefing on and a
// blue sheep within 16 blocks (4 up and down).
func (g *useSpell) canWololo(m *javamob.Mob) bool {
	if _, ok := m.Target(); ok || g.e.caster.IsCasting() || m.TickCount() < g.s.NextAttackTick {
		return false
	}
	var sheep []world.Entity
	for s := range m.Tx().EntitiesWithin(m.BBox().GrowVec3(mgl64.Vec3{16, 4, 16})) {
		if s.H().Type().EncodeEntity() != "minecraft:sheep" || s.Position().Sub(m.Position()).Len() > 16 {
			continue
		}
		if c, ok := sheepColour(s); ok && c == item.ColourBlue() {
			sheep = append(sheep, s)
		}
	}
	if len(sheep) == 0 {
		return false
	}
	g.e.wololo = sheep[m.Rand().IntN(len(sheep))].H()
	return true
}

// sheepColour reads the wool colour of a Java sheep.
func sheepColour(e world.Entity) (item.Colour, bool) {
	m, ok := javamob.Of(e)
	if !ok {
		return item.Colour{}, false
	}
	c, ok := m.Behaviour().(interface{ Colour() item.Colour })
	if !ok {
		return item.Colour{}, false
	}
	return c.Colour(), true
}

// wololoCast is EvokerWololoSpellGoal.performSpellCasting: the blue sheep turns red. The Java sheep (jfarm) has no
// colour setter yet; a sheep behaviour with SetColour(m, colour) is turned red.
func (e *Evoker) wololoCast(m *javamob.Mob) {
	if e.wololo == nil {
		return
	}
	s, ok := e.wololo.Entity(m.Tx())
	if !ok {
		return
	}
	sm, ok := javamob.Of(s)
	if !ok || sm.Dead() {
		return
	}
	if c, ok := sm.Behaviour().(interface {
		SetColour(*javamob.Mob, item.Colour)
	}); ok {
		c.SetColour(sm, item.ColourRed())
	}
}

// canSummon is the extra check of EvokerSummonSpellGoal.canUse: fewer vexes within 16 blocks than a random 1-8.
func (e *Evoker) canSummon(m *javamob.Mob) bool {
	n := 0
	for v := range m.Tx().EntitiesWithin(m.BBox().Grow(16)) {
		if v.H().Type().EncodeEntity() == VexID && v.Position().Sub(m.Position()).Len() <= 16 {
			n++
		}
	}
	return jraid.CanSummonVexes(m.Rand(), n)
}

// summonVexes is EvokerSummonSpellGoal.performSpellCasting: three vexes bound to where they appear, living 30 to
// 119 seconds.
func (e *Evoker) summonVexes(m *javamob.Mob) {
	for i := 0; i < jraid.VexSummons; i++ {
		pos, life := jraid.VexSpawn(m.Rand(), cube.PosFromVec3(m.Position()))
		owner := m.H()
		_, _ = javamob.Spawn(m.Tx(), VexID, pos.Vec3(), javamob.SpawnMobSummoned, javamob.SpawnOptions{Configure: func(v *javamob.Mob) {
			if vx, ok := v.Behaviour().(*Vex); ok {
				vx.owner = owner
				b := pos
				vx.bound = &b
				vx.limited, vx.life = true, life
			}
		}})
	}
}

// fangAttack is EvokerAttackSpellGoal.performSpellCasting: fangs in a ring around the evoker or in a line to the
// target. A Crossfly fang (misc.SpawnFang) bites two ticks after it appears, so each is raised six ticks after its
// Java warm-up: the bite lands on the Java tick (warm-up + 8).
func (e *Evoker) fangAttack(m *javamob.Mob) {
	t, ok := m.Target()
	if !ok {
		return
	}
	p, tp := m.Position(), t.Position()
	minY := math.Min(tp[1], p[1])
	maxY := math.Max(tp[1], p[1]) + 1
	g := fangGround{tx: m.Tx()}
	for _, f := range jraid.FangLayout(p[0], p[2], tp[0], tp[2], tp.Sub(p).LenSqr()) {
		y, ok := jraid.FangY(g, f.X, f.Z, minY, maxY)
		if !ok {
			continue
		}
		e.fangs = append(e.fangs, pendingFang{pos: mgl64.Vec3{f.X, y, f.Z}, delay: f.Delay + 6})
	}
}

// fangGround reads the blocks under the fangs.
type fangGround struct{ tx *world.Tx }

func (g fangGround) TopFaceSturdy(pos cube.Pos) bool {
	return g.tx.Block(pos).Model().FaceSolid(pos, cube.FaceUp, g.tx)
}

func (g fangGround) CollisionTop(pos cube.Pos) float64 {
	top := 0.0
	for _, bb := range g.tx.Block(pos).Model().BBox(pos, g.tx) {
		top = math.Max(top, bb.Max()[1])
	}
	return top
}
