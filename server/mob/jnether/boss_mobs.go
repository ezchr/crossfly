//go:build crossfly_full

package jnether

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/boss"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// The two bosses as Java mob types: the logic of dragon*.go and wither.go
// on the Java mob base.
//
// The base has no hook to replace aiStep, travel or the 20-tick death
// timer, so the dragon runs its whole EnderDragon.aiStep in AiStep (before
// the base's), keeps its own motion and hands the base a zero velocity
// (NoGravity: the base moves it nowhere), puts the motion back for the
// clients in PostTick, and plays tickDeath (200 ticks rising, XP) while it
// still has 1 health, dying for real at its end. Java clients therefore see
// the rising dragon without the death beams, and the phase as sitting or
// flying only (EnderDragon.DATA_PHASE needs the client getter forwarding
// asked of the base).

func init() {
	javamob.Register(javamob.Type{
		ID: DragonID, Category: javamob.CategoryMonster,
		Width: 16, Height: 8, EyeHeight: 8 * 0.85,
		FireImmune: true, NoLootTable: true,
		Attributes: func() map[javamob.Attribute]float64 {
			a := javamob.LivingAttributes()
			a[javamob.AttrMaxHealth] = 200
			return a
		},
		Families: []string{"dragon", "mob"},
		New:      func() javamob.Behaviour { return &dragonMob{d: NewDragon()} },
	})
	javamob.Register(javamob.Type{
		ID: WitherID, Category: javamob.CategoryMonster,
		Width: 0.9, Height: 3.5, EyeHeight: 3.5 * 0.85,
		FireImmune: true, NotInPeaceful: true, Undead: true,
		Attributes: func() map[javamob.Attribute]float64 {
			a := javamob.MonsterAttributes()
			a[javamob.AttrMaxHealth] = 300
			a[javamob.AttrMovementSpeed] = float64(float32(0.6))
			a[javamob.AttrFlyingSpeed] = float64(float32(0.6))
			a[javamob.AttrFollowRange] = 40
			a[javamob.AttrArmor] = 4
			return a
		},
		Families: []string{"wither", "undead", "monster", "mob"},
		XPReward: 50,
		New:      func() javamob.Behaviour { return &witherMob{w: NewWither()} },
	})
}

// dragonMob is EnderDragon on the base.
type dragonMob struct {
	d   *Dragon
	vel mgl64.Vec3
	// animating is set while tickDeath plays (the dragon still has 1 health).
	animating bool
}

// dragonBody is the dragon's view of its mob: its own motion instead of the
// base's.
type dragonBody struct {
	*javamob.Mob
	b *dragonMob
}

func (b dragonBody) Velocity() mgl64.Vec3     { return b.b.vel }
func (b dragonBody) SetVelocity(v mgl64.Vec3) { b.b.vel = v }

// SetHealth: health 0 (the death flight reached the podium) starts the
// death animation instead.
func (b dragonBody) SetHealth(v float64) {
	if v <= 0 {
		b.b.animating = true
		b.Mob.SetHealth(1)
		return
	}
	b.Mob.SetHealth(v)
}

func (b *dragonMob) RegisterGoals(m *javamob.Mob) {
	m.UpdateMeta(func(meta *mob.Meta) { meta.NoGravity = true })
	b.vel = m.Velocity()
}

// AiStep is EnderDragon.aiStep (or tickDeath while dying).
func (b *dragonMob) AiStep(m *javamob.Mob) {
	body := dragonBody{Mob: m, b: b}
	if b.animating {
		if b.d.TickDeath(body) {
			b.animating = false
			m.Kill(entity.VoidDamageSource{})
		}
	} else if !m.Dead() {
		b.d.AiStep(body)
	}
	syncDragon(m, b.d)
	// The base moves the mob by its velocity: nothing.
	m.SetVelocity(mgl64.Vec3{})
}

// PostTick shows the dragon's own motion to the clients.
func (b *dragonMob) PostTick(m *javamob.Mob) {
	if !b.animating {
		m.SetVelocity(b.vel)
	}
}

// FilterDamage is EnderDragon.hurt(part, ...).
func (b *dragonMob) FilterDamage(m *javamob.Mob, d *mob.Damage) {
	if b.animating {
		d.Cancel = true
		return
	}
	if p, ok := m.TakeHitPart(); ok {
		b.d.SetHitPart(p)
	}
	amount, ok := b.d.FilterDamage(dragonBody{Mob: m, b: b}, d.Source, d.Amount)
	if !ok {
		d.Cancel = true
		return
	}
	d.Amount = amount
}

func (b *dragonMob) RemoveWhenFarAway(*javamob.Mob, float64) bool { return false }

// EncodeNBT is addAdditionalSaveData.
func (b *dragonMob) EncodeNBT(data map[string]any) { b.d.EncodeNBT(data) }

// DecodeNBT is readAdditionalSaveData.
func (b *dragonMob) DecodeNBT(m *javamob.Mob, data map[string]any) {
	b.d.DecodeNBT(dragonBody{Mob: m, b: b}, data)
	if b.d.Phase() == PhaseDying && b.d.DeathTime() > 0 {
		b.animating = true
	}
}

// DragonPhase is DATA_PHASE for the Java client.
func (b *dragonMob) DragonPhase() int32 { return int32(b.d.Phase()) }

// syncDragon shows the sitting phases to both clients (Meta.Sitting).
func syncDragon(m *javamob.Mob, d *Dragon) {
	if s := d.Sitting(); m.Meta().Sitting != s {
		m.UpdateMeta(func(meta *mob.Meta) { meta.Sitting = s })
	}
}

// fightAdapter is the End fight as the Java dragon sees it, with the
// fight's boss bar (EnderDragonFight.dragonEvent: pink, boss music, fog).
type fightAdapter struct {
	f   *boss.EndFight
	bar bossBar
}

func (a *fightAdapter) AliveCrystals() int     { return a.f.AliveCrystals() }
func (a *fightAdapter) PreviouslyKilled() bool { return a.f.PreviouslyKilled() }

func (a *fightAdapter) DragonKilled(tx *world.Tx, e world.Entity) {
	a.bar.hide(e)
	a.f.JavaDragonKilled(tx, e)
}

func (a *fightAdapter) UpdateDragon(tx *world.Tx, e world.Entity, health, maxHealth float64) {
	a.bar.update(tx, e, health/maxHealth, tx.CurrentTick()%20 == 0, withinDragonFight)
}

// javaDragonHooks makes the End fight spawn and adopt Java dragons.
type javaDragonHooks struct{}

func (javaDragonHooks) Spawn(tx *world.Tx, pos mgl64.Vec3, yaw float64) world.Entity {
	m, err := javamob.Spawn(tx, DragonID, pos, javamob.SpawnEvent, javamob.SpawnOptions{Rotation: cube.Rotation{yaw, 0}})
	if err != nil {
		return nil
	}
	if b, ok := m.Behaviour().(*dragonMob); ok {
		b.d.SetPhase(dragonBody{Mob: m, b: b}, PhaseHoldingPattern)
	}
	return m
}

func (javaDragonHooks) Attach(e world.Entity, f *boss.EndFight) bool {
	m, ok := javamob.Of(e)
	if !ok {
		return false
	}
	b, ok := m.Behaviour().(*dragonMob)
	if !ok {
		return false
	}
	if b.d.Fight == nil {
		b.d.Fight = &fightAdapter{f: f, bar: bossBar{colour: boss.ColourPink}}
	}
	return true
}

// witherMob is WitherBoss on the base.
type witherMob struct {
	w      *Wither
	ranged *witherRanged
}

// RegisterGoals is WitherBoss.registerGoals. (FlyingPathNavigation is not
// in the base yet: WaterAvoidingRandomFlyingGoal is left out, so an idle
// wither does not roam; with a target it flies by its aiStep.)
func (b *witherMob) RegisterGoals(m *javamob.Mob) {
	m.Goals().Add(0, &witherDoNothing{GoalBase: javamob.GoalBase{Control: javamob.FlagMove | javamob.FlagJump | javamob.FlagLook}, w: b.w})
	b.ranged = &witherRanged{GoalBase: javamob.GoalBase{Control: javamob.FlagMove | javamob.FlagLook}, w: b.w, interval: 40, radius: 20}
	m.Goals().Add(2, b.ranged)
	m.Goals().Add(6, goal.NewLookAtPlayer(8))
	m.Goals().Add(7, goal.NewRandomLookAround())
	m.Targets().Add(1, goal.NewHurtByTarget())
	t := goal.NewNearestAttackableTarget(func(e world.Entity) bool { return b.w.CanAttack(nil, e) }, false, false)
	t.RandomInterval = 0
	m.Targets().Add(2, t)
	m.UpdateMeta(func(meta *mob.Meta) { meta.NoGravity = true })
}

func (b *witherMob) AiStep(m *javamob.Mob) { b.w.AiStep(m) }

// PostTick turns the side heads (the end of aiStep) and shows the heads'
// targets and the spawn charge.
func (b *witherMob) PostTick(m *javamob.Mob) {
	b.w.PostAiStep(m)
	syncWither(m, b.w)
}

func (b *witherMob) CustomServerAiStep(m *javamob.Mob) { b.w.CustomServerAiStep(m) }

func (b *witherMob) FilterDamage(m *javamob.Mob, d *mob.Damage) {
	if fireImmune(d); d.Cancel {
		return
	}
	if !b.w.FilterDamage(m, d.Source) {
		d.Cancel = true
	}
}

func (b *witherMob) DropCustomDeathLoot(m *javamob.Mob, _ world.DamageSource, _ bool) {
	b.w.DeathDrops(m.Tx(), m.Position())
}

func (b *witherMob) OnDeath(m *javamob.Mob, _ world.DamageSource) { b.w.RemoveBar(m) }

// EncodeNBT is addAdditionalSaveData.
func (b *witherMob) EncodeNBT(data map[string]any) { b.w.EncodeNBT(data) }

// DecodeNBT is readAdditionalSaveData.
func (b *witherMob) DecodeNBT(_ *javamob.Mob, data map[string]any) { b.w.DecodeNBT(data) }

// witherDoNothing is WitherBoss.WitherDoNothingGoal: nothing moves while it
// charges up.
type witherDoNothing struct {
	javamob.GoalBase
	w *Wither
}

func (g *witherDoNothing) CanUse(*javamob.Mob) bool { return g.w.InvulnerableTicks() > 0 }

// witherRanged is RangedAttackGoal(wither, 1.0, 40, 20): close in to 20
// blocks when the target is not seen, shoot every 40 ticks (scaled by
// distance, at least 40) with the middle head.
type witherRanged struct {
	javamob.GoalBase
	w                *Wither
	interval         int
	radius           float64
	attackTime, seen int
}

func (g *witherRanged) RequiresUpdateEveryTick() bool { return true }

func (g *witherRanged) CanUse(m *javamob.Mob) bool {
	t, ok := m.Target()
	return ok && alive(t)
}

func (g *witherRanged) CanContinueToUse(m *javamob.Mob) bool {
	return g.CanUse(m) || !m.Navigation().Done()
}

func (g *witherRanged) Start(*javamob.Mob) { g.attackTime = -1 }

func (g *witherRanged) Stop(*javamob.Mob) { g.seen, g.attackTime = 0, -1 }

func (g *witherRanged) Tick(m *javamob.Mob) {
	t, ok := m.Target()
	if !ok {
		return
	}
	dist := t.Position().Sub(m.Position()).LenSqr()
	if m.Sensing().HasLineOfSight(m, t) {
		g.seen++
	} else {
		g.seen = 0
	}
	m.LookControl().SetLookAtEntity(t, 30, 30)
	if g.attackTime--; g.attackTime == 0 {
		if g.seen == 0 {
			return
		}
		g.w.ShootAt(m, 0, t)
		g.attackTime = int(math.Floor(math.Sqrt(dist) / g.radius * float64(g.interval)))
		g.attackTime = max(g.attackTime, g.interval)
	} else if g.attackTime < 0 {
		g.attackTime = int(math.Floor(math.Sqrt(dist) / g.radius * float64(g.interval)))
		g.attackTime = max(g.attackTime, g.interval)
	}
}

// syncWither puts the head targets and the spawn charge where both clients
// read them (the Bedrock actor data of the Bedrock wither; javasession maps
// them to WitherBoss DATA_TARGET_A..C and DATA_ID_INV).
func syncWither(m *javamob.Mob, w *Wither) {
	m.SetActorData(protocol.EntityDataKeyTargetA, w.HeadTarget(0))
	m.SetActorData(protocol.EntityDataKeyTargetB, w.HeadTarget(1))
	m.SetActorData(protocol.EntityDataKeyTargetC, w.HeadTarget(2))
	m.SetActorData(protocol.EntityDataKeyInvulnerableTicks, int32(w.InvulnerableTicks()))
}

// spawnWither is the summoning hook: a Java wither, invulnerable while it
// charges up.
func spawnWither(tx *world.Tx, pos mgl64.Vec3, yaw float64) bool {
	m, err := javamob.Spawn(tx, WitherID, pos, javamob.SpawnTriggered, javamob.SpawnOptions{
		Rotation: cube.Rotation{yaw, 0},
		Configure: func(m *javamob.Mob) {
			m.SetBodyYaw(yaw)
			if b, ok := m.Behaviour().(*witherMob); ok {
				b.w.MakeInvulnerable(m)
			}
		},
	})
	return err == nil && m != nil
}
