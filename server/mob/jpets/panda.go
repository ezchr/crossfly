//go:build crossfly_full

package jpets

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/janimal"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// Pandas (animal/panda/Panda.java): genes and personalities (lazy, worried,
// playful, brown, weak, aggressive), eating bamboo and cake sitting down,
// sneezing cubs, rolling, lying on the back.

func init() {
	javamob.Register(javamob.Type{
		ID: "minecraft:panda", Category: javamob.CategoryCreature, Width: 1.3, Height: 1.25,
		Attributes: withAttr(animalAttributes(20, f32(0.15)), javamob.AttrAttackDamage, 6),
		Families:   []string{"panda", "mob"},
		New:        func() javamob.Behaviour { return &Panda{} },
	})
}

// Panda flags (DATA_ID_FLAGS).
const (
	flagSneeze = 2
	flagRoll   = 4
	flagSit    = 8
	flagOnBack = 16
)

// Panda is the behaviour of a panda.
type Panda struct {
	Animal janimal.Animal
	Genes  PandaGenes

	flags          byte
	unhappyCounter int
	sneezeCounter  int
	eatCounter     int
	gotBamboo      bool
	didBite        bool
	rollCounter    int
	rollDelta      mgl64.Vec3

	look  *pandaLookAtPlayer
	panic *janimal.PanicGoal[petHost]
	self  *world.EntityHandle
}

func (p *Panda) animal() *janimal.Animal { return &p.Animal }
func (p *Panda) panicking() bool         { return p.panic != nil && p.panic.Running() }

// personality is Panda.getVariant.
func (p *Panda) personality() Gene { return PandaPersonality(p.Genes.Main, p.Genes.Hidden) }

func (p *Panda) flag(b byte) bool { return p.flags&b != 0 }

// setFlag sets a flag and its Bedrock flag (javasession maps both to Java).
func (p *Panda) setFlag(m *javamob.Mob, b byte, v bool) {
	if v {
		p.flags |= b
	} else {
		p.flags &^= b
	}
	switch b {
	case flagSneeze:
		m.SetActorFlag(protocol.EntityDataFlagSneezing, v)
	case flagRoll:
		m.SetActorFlag(protocol.EntityDataFlagRolling, v)
	case flagSit:
		if m.Meta().Sitting != v {
			m.UpdateMeta(func(meta *mob.Meta) { meta.Sitting = v })
		}
	case flagOnBack:
		m.SetActorFlag(protocol.EntityDataFlagLayingDown, v)
	}
}

// eating is isEating; setEating eat().
func (p *Panda) eating() bool { return p.eatCounter > 0 }
func (p *Panda) setEating(m *javamob.Mob, v bool) {
	if v {
		p.eatCounter = 1
	} else {
		p.eatCounter = 0
	}
	m.SetActorFlag(protocol.EntityDataFlagEating, v)
}

// scared is Panda.isScared: a worried panda in a thunderstorm.
func (p *Panda) scared(m *javamob.Mob) bool {
	return p.personality() == GeneWorried && m.Tx().Thundering()
}

// canPerformAction is Panda.canPerformAction.
func (p *Panda) canPerformAction(m *javamob.Mob) bool {
	return !p.flag(flagOnBack) && !p.scared(m) && !p.eating() && !p.flag(flagRoll) && !p.flag(flagSit)
}

// setGenes shows the genes (Bedrock shows the personality as Variant;
// javasession sends it as both Java genes).
func (p *Panda) setGenes(m *javamob.Mob, g PandaGenes) {
	p.Genes = g
	m.UpdateMeta(func(meta *mob.Meta) { meta.Variant = int32(p.personality()) })
}

// setAttributes is Panda.setAttributes: weak pandas have 10 health, lazy
// ones speed 0.07.
func (p *Panda) setAttributes(m *javamob.Mob) {
	switch p.personality() {
	case GeneWeak:
		m.Attributes().Instance(javamob.AttrMaxHealth).SetBase(10)
	case GeneLazy:
		m.Attributes().Instance(javamob.AttrMovementSpeed).SetBase(f32(0.07))
	}
	m.SyncAttributes()
}

// RegisterGoals is Panda.registerGoals.
func (p *Panda) RegisterGoals(m *javamob.Mob) {
	p.self = m.H()
	if !m.Baby() {
		m.SetCanPickUpLoot(true)
	}
	gs, ts := m.Goals(), m.Targets()
	gs.Add(0, goal.NewFloat(m))
	p.panic = &janimal.PanicGoal[petHost]{Speed: 2, ShouldPanic: func(h petHost) bool { return environmentalPanic[h.LastDamageType()] }}
	gs.Add(2, &pandaPanic{animalGoal: animalGoal{g: p.panic}, p: p})
	gs.Add(2, &pandaBreed{animalGoal: animalGoal{g: &janimal.BreedGoal[petHost]{Speed: 1}}, p: p})
	attack := goal.NewMeleeAttack(float64(float32(1.2)), true)
	gs.Add(3, &pandaAttack{MeleeAttack: attack, p: p})
	gs.Add(4, wrapGoal(janimal.NewTemptGoal[petHost](1, stackNamed("minecraft:bamboo"), false)))
	gs.Add(6, &pandaAvoid{AvoidEntity: goal.NewAvoidEntity(func(e world.Entity) bool {
		pl, ok := e.(*player.Player)
		return ok && pl.GameMode() != world.GameModeSpectator
	}, 8, 2, 2), p: p})
	gs.Add(6, &pandaAvoid{AvoidEntity: goal.NewAvoidEntity(func(e world.Entity) bool {
		jm, ok := javamob.Of(e)
		return ok && jm.HasFamily("monster")
	}, 4, 2, 2), p: p})
	gs.Add(7, &pandaSit{p: p})
	gs.Add(8, &pandaLieOnBack{p: p})
	gs.Add(8, &pandaSneeze{p: p})
	p.look = &pandaLookAtPlayer{p: p, LookAt: goal.NewLookAtPlayer(6)}
	gs.Add(9, p.look)
	gs.Add(10, goal.NewRandomLookAround())
	gs.Add(12, &pandaRollGoal{p: p})
	gs.Add(13, wrapGoal(&janimal.FollowParentGoal[petHost]{Speed: 1.25}))
	gs.Add(14, goal.NewWaterAvoidingRandomStroll(1, 0.001))
	hurt := goal.NewHurtByTarget().SetAlertOthers()
	hurt.AlertOther = func(_, other *javamob.Mob, t world.Entity) {
		if op, ok := other.Behaviour().(*Panda); ok && op.personality() == GeneAggressive {
			other.SetTarget(t)
		}
	}
	ts.Add(1, &pandaHurtBy{HurtByTarget: hurt, p: p})
}

// FinalizeSpawn is Panda.finalizeSpawn: two random genes, the attributes.
func (p *Panda) FinalizeSpawn(m *javamob.Mob, reason javamob.SpawnReason) {
	p.setGenes(m, PandaGenes{RandomGene(m.Rand()), RandomGene(m.Rand())})
	p.setAttributes(m)
	if (reason == javamob.SpawnNatural || reason == javamob.SpawnChunkGeneration) && m.Rand().Float32() < 0.2 {
		p.Animal.SetBaby(true)
		m.SetBaby(true)
	}
}

// DoHurtTarget is Panda.doHurtTarget: a panda that is not aggressive bit
// once and gives up (didBite).
func (p *Panda) DoHurtTarget(m *javamob.Mob, t world.Entity) bool {
	if p.personality() != GeneAggressive {
		p.didBite = true
	}
	m.PlaySound("bite")
	return m.BaseDoHurtTarget(t)
}

// FilterDamage is Panda.hurtServer: a hit panda stands up.
func (p *Panda) FilterDamage(m *javamob.Mob, d *mob.Damage) { p.setFlag(m, flagSit, false) }

// OnHurt: love ends.
func (p *Panda) OnHurt(m *javamob.Mob, dealt float64, src world.DamageSource) { p.Animal.ResetLove() }

// AiStep: age, love, picking up bamboo and cake (canPickUpAndEat).
func (p *Panda) AiStep(m *javamob.Mob) {
	animalTick(m, &p.Animal)
	if m.Dead() || m.Baby() || !m.ItemBySlot(javamob.SlotMainHand).Empty() {
		return
	}
	for _, e := range entitiesTouching(m.Tx(), m.BBox().GrowVec3(mgl64.Vec3{1, 0, 1})) {
		ent, ok := e.(*entity.Ent)
		if !ok {
			continue
		}
		ib, ok := ent.Behaviour().(*entity.ItemBehaviour)
		if !ok || !ib.PickupReady() || !pandaEatsFromGround(itemName(ib.Item())) {
			continue
		}
		m.SetItemSlot(javamob.SlotMainHand, ib.Item())
		m.SetGuaranteedDrop(javamob.SlotMainHand)
		_ = ent.Close()
		return
	}
}

// pandaEatsFromGround is ItemTags.PANDA_EATS_FROM_GROUND.
func pandaEatsFromGround(name string) bool {
	return name == "minecraft:bamboo" || name == "minecraft:cake"
}

// PostTick is Panda.tick.
func (p *Panda) PostTick(m *javamob.Mob) {
	if p.personality() == GeneWorried {
		if m.Tx().Thundering() && !m.InWater() {
			p.setFlag(m, flagSit, true)
			p.setEating(m, false)
		} else if !p.eating() {
			p.setFlag(m, flagSit, false)
		}
	}
	t, hasTarget := m.Target()
	if !hasTarget {
		p.gotBamboo, p.didBite = false, false
	}
	if p.unhappyCounter > 0 {
		if hasTarget {
			m.LookAtEntity(t, 90, 90)
		}
		if p.unhappyCounter == 29 || p.unhappyCounter == 14 {
			m.PlaySound("cant_breed")
		}
		p.unhappyCounter--
	}
	if p.flag(flagSneeze) {
		p.sneezeCounter++
		if p.sneezeCounter > 20 {
			p.setFlag(m, flagSneeze, false)
			p.sneezeCounter = 0
			p.afterSneeze(m)
		} else if p.sneezeCounter == 1 {
			m.PlaySound("presneeze")
		}
	}
	if p.flag(flagRoll) {
		p.handleRoll(m)
	} else {
		p.rollCounter = 0
	}
	if p.flag(flagSit) {
		m.SetXRot(0)
	}
	p.handleEating(m)
}

// handleRoll is Panda.handleRoll: 32 ticks rolling forward with three hops.
func (p *Panda) handleRoll(m *javamob.Mob) {
	p.rollCounter++
	if p.rollCounter > 32 {
		p.setFlag(m, flagRoll, false)
		return
	}
	v := m.Velocity()
	switch p.rollCounter {
	case 1:
		mul := float32(0.2)
		if m.Baby() {
			mul = 0.1
		}
		p.rollDelta = mgl64.Vec3{v[0] + float64(-sinDeg32(m.YRot())*mul), 0, v[2] + float64(cosDeg32(m.YRot())*mul)}
		m.SetVelocity(p.rollDelta.Add(mgl64.Vec3{0, 0.27, 0}))
	case 7, 15, 23:
		y := v[1]
		if m.OnGround() {
			y = 0.27
		}
		m.SetVelocity(mgl64.Vec3{0, y, 0})
	default:
		m.SetVelocity(mgl64.Vec3{p.rollDelta[0], v[1], p.rollDelta[2]})
	}
}

// afterSneeze is Panda.afterSneeze: grown pandas within 10 blocks jump, and
// one sneeze in 700 drops a slime ball (gameplay/panda_sneeze).
func (p *Panda) afterSneeze(m *javamob.Mob) {
	m.PlaySound("sneeze")
	for _, e := range entitiesTouching(m.Tx(), m.BBox().Grow(10)) {
		o, ok := javamob.Of(e)
		if !ok {
			continue
		}
		op, ok := o.Behaviour().(*Panda)
		if ok && !o.Baby() && o.OnGround() && !o.InWater() && op.canPerformAction(o) {
			o.JumpControl().Jump()
		}
	}
	if m.Rand().IntN(700) == 0 {
		if it, ok := world.ItemByName("minecraft:slime_ball", 0); ok {
			m.SpawnAtLocation(item.NewStack(it, 1))
		}
	}
}

// handleEating is Panda.handleEating: a sitting panda with food starts
// eating (1 in 80); after 80 ticks it stops now and then, finishing bamboo
// or cake after 100.
func (p *Panda) handleEating(m *javamob.Mob) {
	held := m.ItemBySlot(javamob.SlotMainHand)
	if !p.eating() && p.flag(flagSit) && !p.scared(m) && !held.Empty() && m.Rand().IntN(80) == 1 {
		p.setEating(m, true)
	} else if held.Empty() || !p.flag(flagSit) {
		p.setEating(m, false)
	}
	if !p.eating() {
		return
	}
	if p.eatCounter%5 == 0 {
		m.PlaySound("eat")
	}
	if p.eatCounter > 80 && m.Rand().IntN(20) == 1 {
		if p.eatCounter > 100 && pandaEatsFromGround(itemName(held)) {
			m.SetItemSlot(javamob.SlotMainHand, item.Stack{})
			p.setFlag(m, flagSit, false)
		}
		p.setEating(m, false)
		return
	}
	p.eatCounter++
}

// tryToSit is Panda.tryToSit.
func (p *Panda) tryToSit(m *javamob.Mob) {
	if !m.InWater() {
		m.SetZza(0)
		m.Navigation().Stop()
		p.setFlag(m, flagSit, true)
	}
}

// MobInteract is Panda.mobInteract.
func (p *Panda) MobInteract(m *javamob.Mob, user item.User, held item.Stack) (item.Stack, bool) {
	pl, ok := user.(*player.Player)
	if !ok || p.scared(m) {
		return held, false
	}
	if p.flag(flagOnBack) {
		p.setFlag(m, flagOnBack, false)
		return held, true
	}
	if itemName(held) != "minecraft:bamboo" {
		return held, false
	}
	if _, ok := m.Target(); ok {
		p.gotBamboo = true
	}
	if p.Animal.CanAgeUp() {
		if p.Animal.AgeUp(int32(float32(-p.Animal.Age()/20)*0.1), true) {
			m.SetBaby(p.Animal.Baby())
		}
		return consumeOne(user, held), true
	}
	if m.Baby() {
		return held, false
	}
	if p.Animal.Age() == 0 && p.Animal.CanFallInLove() {
		p.Animal.SetInLove(pl.UUID())
		broadcastEvent(m, janimal.Event(janimal.EventLoveHearts))
		return consumeOne(user, held), true
	}
	if p.flag(flagSit) || m.InWater() {
		return held, false
	}
	p.tryToSit(m)
	p.setEating(m, true)
	if cur := m.ItemBySlot(javamob.SlotMainHand); !cur.Empty() && !isCreative(user) {
		m.SpawnAtLocation(cur)
	}
	m.SetItemSlot(javamob.SlotMainHand, held.Grow(1-held.Count()))
	return consumeOne(user, held), true
}

// breed is Panda.getBreedOffspring: genes from both parents.
func (p *Panda) breed(m, partner *javamob.Mob) bool {
	baby, err := javamob.Spawn(m.Tx(), "minecraft:panda", m.Position(), javamob.SpawnBreeding, javamob.SpawnOptions{})
	if err != nil {
		return false
	}
	bp := baby.Behaviour().(*Panda)
	pg := partner.Behaviour().(*Panda).Genes
	bp.setGenes(baby, PandaBabyGenes(m.Rand(), p.Genes, &pg))
	bp.setAttributes(baby)
	bp.Animal.SetBaby(true)
	baby.SetBaby(true)
	return true
}

func (p *Panda) EncodeNBT(data map[string]any) {
	p.Animal.EncodeNBT(data)
	data["MainGene"] = p.Genes.Main.String()
	data["HiddenGene"] = p.Genes.Hidden.String()
}

func (p *Panda) DecodeNBT(m *javamob.Mob, data map[string]any) {
	p.Animal.DecodeNBT(data)
	m.SetBaby(p.Animal.Baby())
	main, _ := data["MainGene"].(string)
	hidden, _ := data["HiddenGene"].(string)
	p.setGenes(m, PandaGenes{GeneByName(main), GeneByName(hidden)})
}

// CustomServerAiStep: PandaMoveControl only moves a panda that can act.
func (p *Panda) CustomServerAiStep(m *javamob.Mob) {
	if !p.canPerformAction(m) {
		m.MoveControl().SetWait()
	}
}

// ---- goals ----

type pandaPanic struct {
	animalGoal
	p *Panda
}

// CanContinueToUse is PandaPanicGoal.canContinueToUse: sitting ends it.
func (g *pandaPanic) CanContinueToUse(m *javamob.Mob) bool {
	if g.p.flag(flagSit) {
		m.Navigation().Stop()
		return false
	}
	return g.animalGoal.CanContinueToUse(m)
}

// pandaBreed is PandaBreedGoal: only with bamboo within 8 blocks (3 up);
// without, the panda is unhappy (32 ticks, again after 30 s) and looks at
// the nearest player.
type pandaBreed struct {
	animalGoal
	p               *Panda
	unhappyCooldown int
}

func (g *pandaBreed) CanUse(m *javamob.Mob) bool {
	if !g.animalGoal.CanUse(m) || g.p.unhappyCounter != 0 {
		return false
	}
	if canFindBamboo(m) {
		return true
	}
	if g.unhappyCooldown <= m.TickCount() {
		g.p.unhappyCounter = 32
		g.unhappyCooldown = m.TickCount() + 600
		if pl, ok := m.NearestPlayer(javamob.ForNonCombat(8)); ok {
			g.p.look.target = pl.H()
		}
	}
	return false
}

func canFindBamboo(m *javamob.Mob) bool {
	base := cube.PosFromVec3(m.Position())
	for y := 0; y < 3; y++ {
		for r := 0; r < 8; r++ {
			for x := 0; x <= r; {
				z := 0
				if x < r && x > -r {
					z = r
				}
				for z <= r {
					if _, ok := m.Tx().Block(base.Add(cube.Pos{x, y, z})).(block.Bamboo); ok {
						return true
					}
					if z > 0 {
						z = -z
					} else {
						z = 1 - z
					}
				}
				if x > 0 {
					x = -x
				} else {
					x = 1 - x
				}
			}
		}
	}
	return false
}

type pandaAttack struct {
	*goal.MeleeAttack
	p *Panda
}

func (g *pandaAttack) CanUse(m *javamob.Mob) bool {
	return g.p.canPerformAction(m) && g.MeleeAttack.CanUse(m)
}

// pandaAvoid is PandaAvoidGoal: only worried pandas flee.
type pandaAvoid struct {
	*goal.AvoidEntity
	p *Panda
}

func (g *pandaAvoid) CanUse(m *javamob.Mob) bool {
	return g.p.personality() == GeneWorried && g.p.canPerformAction(m) && g.AvoidEntity.CanUse(m)
}

// pandaHurtBy is PandaHurtByTargetGoal: bamboo or a bite ends the fight.
type pandaHurtBy struct {
	*goal.HurtByTarget
	p *Panda
}

func (g *pandaHurtBy) CanContinueToUse(m *javamob.Mob) bool {
	if !g.p.gotBamboo && !g.p.didBite {
		return g.HurtByTarget.CanContinueToUse(m)
	}
	m.SetTarget(nil)
	return false
}

// pandaLieOnBack is PandaLieOnBackGoal: lazy pandas roll onto their back.
type pandaLieOnBack struct {
	p        *Panda
	cooldown int
}

func (g *pandaLieOnBack) Flags() javamob.Flags { return 0 }
func (g *pandaLieOnBack) CanUse(m *javamob.Mob) bool {
	return g.cooldown < m.TickCount() && g.p.personality() == GeneLazy && g.p.canPerformAction(m) &&
		m.Rand().IntN(javamob.ReducedTickDelay(400)) == 1
}
func (g *pandaLieOnBack) CanContinueToUse(m *javamob.Mob) bool {
	return !m.InWater() && (g.p.personality() == GeneLazy || m.Rand().IntN(javamob.ReducedTickDelay(600)) != 1) &&
		m.Rand().IntN(javamob.ReducedTickDelay(2000)) != 1
}
func (g *pandaLieOnBack) Start(m *javamob.Mob) {
	g.p.setFlag(m, flagOnBack, true)
	g.cooldown = 0
}
func (g *pandaLieOnBack) Stop(m *javamob.Mob) {
	g.p.setFlag(m, flagOnBack, false)
	g.cooldown = m.TickCount() + 200
}

// pandaLookAtPlayer is PandaLookAtPlayerGoal: an unhappy panda is made to
// look at a player.
type pandaLookAtPlayer struct {
	*goal.LookAt
	p      *Panda
	target *world.EntityHandle
}

func (g *pandaLookAtPlayer) CanUse(m *javamob.Mob) bool {
	if g.target != nil {
		return g.p.canPerformAction(m) && m.Rand().Float32() < g.Probability
	}
	return g.LookAt.CanUse(m) && g.p.canPerformAction(m)
}

func (g *pandaLookAtPlayer) Tick(m *javamob.Mob) {
	if g.target != nil {
		if t, ok := g.target.Entity(m.Tx()); ok {
			m.LookControl().SetLookAtEntity(t, 10, maxHeadXRot(m))
			return
		}
	}
	g.LookAt.Tick(m)
}

func (g *pandaLookAtPlayer) Stop(m *javamob.Mob) {
	g.target = nil
	g.LookAt.Stop(m)
}

// pandaRollGoal is PandaRollGoal (MOVE, LOOK, JUMP; not interruptible): cubs
// and playful pandas roll over, always towards a drop.
type pandaRollGoal struct{ p *Panda }

func (g *pandaRollGoal) Flags() javamob.Flags {
	return javamob.FlagMove | javamob.FlagLook | javamob.FlagJump
}
func (g *pandaRollGoal) IsInterruptable() bool              { return false }
func (g *pandaRollGoal) CanContinueToUse(*javamob.Mob) bool { return false }
func (g *pandaRollGoal) Start(m *javamob.Mob)               { g.p.setFlag(m, flagRoll, true) }

func (g *pandaRollGoal) CanUse(m *javamob.Mob) bool {
	playful := g.p.personality() == GenePlayful
	if !(m.Baby() || playful) || !m.OnGround() || !g.p.canPerformAction(m) {
		return false
	}
	xd, zd := float64(-sinDeg32(m.YRot())), float64(cosDeg32(m.YRot()))
	step := func(v float64) int {
		if math.Abs(v) > 0.5 {
			return int(math.Copysign(1, v))
		}
		return 0
	}
	ahead := cube.PosFromVec3(m.Position()).Add(cube.Pos{step(xd), -1, step(zd)})
	if _, air := m.Tx().Block(ahead).(block.Air); air {
		return true
	}
	if playful && m.Rand().IntN(javamob.ReducedTickDelay(60)) == 1 {
		return true
	}
	return m.Rand().IntN(javamob.ReducedTickDelay(500)) == 1
}

// pandaSit is PandaSitGoal (MOVE): walk to bamboo or cake on the ground
// within 8 blocks and sit down to eat it; stop now and then, dropping the
// leftovers.
type pandaSit struct {
	p        *Panda
	cooldown int
}

func (g *pandaSit) Flags() javamob.Flags { return javamob.FlagMove }

func (g *pandaSit) groundFood(m *javamob.Mob, r float64) []world.Entity {
	var out []world.Entity
	for _, e := range entitiesTouching(m.Tx(), m.BBox().Grow(r)) {
		if ent, ok := e.(*entity.Ent); ok {
			if ib, ok := ent.Behaviour().(*entity.ItemBehaviour); ok && ib.PickupReady() && pandaEatsFromGround(itemName(ib.Item())) {
				out = append(out, e)
			}
		}
	}
	return out
}

func (g *pandaSit) CanUse(m *javamob.Mob) bool {
	if g.cooldown > m.TickCount() || m.Baby() || m.InWater() || !g.p.canPerformAction(m) || g.p.unhappyCounter > 0 {
		return false
	}
	return !m.ItemBySlot(javamob.SlotMainHand).Empty() || len(g.groundFood(m, 6)) > 0
}

func (g *pandaSit) CanContinueToUse(m *javamob.Mob) bool {
	return !m.InWater() && (g.p.personality() == GeneLazy || m.Rand().IntN(javamob.ReducedTickDelay(600)) != 1) &&
		m.Rand().IntN(javamob.ReducedTickDelay(2000)) != 1
}

func (g *pandaSit) Tick(m *javamob.Mob) {
	if !g.p.flag(flagSit) && !m.ItemBySlot(javamob.SlotMainHand).Empty() {
		g.p.tryToSit(m)
	}
}

func (g *pandaSit) Start(m *javamob.Mob) {
	if m.ItemBySlot(javamob.SlotMainHand).Empty() {
		if food := g.groundFood(m, 8); len(food) > 0 {
			m.Navigation().MoveToEntity(m, food[0], float64(float32(1.2)))
		}
	} else {
		g.p.tryToSit(m)
	}
	g.cooldown = 0
}

func (g *pandaSit) Stop(m *javamob.Mob) {
	if s := m.ItemBySlot(javamob.SlotMainHand); !s.Empty() {
		m.SpawnAtLocation(s)
		m.SetItemSlot(javamob.SlotMainHand, item.Stack{})
		wait := m.Rand().IntN(150) + 10
		if g.p.personality() == GeneLazy {
			wait = m.Rand().IntN(50) + 10
		}
		g.cooldown = m.TickCount() + wait*20
	}
	g.p.setFlag(m, flagSit, false)
}

// pandaSneeze is PandaSneezeGoal: cubs sneeze now and then, weak ones far
// more often.
type pandaSneeze struct{ p *Panda }

func (g *pandaSneeze) Flags() javamob.Flags               { return 0 }
func (g *pandaSneeze) CanContinueToUse(*javamob.Mob) bool { return false }
func (g *pandaSneeze) Start(m *javamob.Mob)               { g.p.setFlag(m, flagSneeze, true) }

func (g *pandaSneeze) CanUse(m *javamob.Mob) bool {
	if !m.Baby() || !g.p.canPerformAction(m) {
		return false
	}
	if g.p.personality() == GeneWeak && m.Rand().IntN(javamob.ReducedTickDelay(500)) == 1 {
		return true
	}
	return m.Rand().IntN(javamob.ReducedTickDelay(6000)) == 1
}
