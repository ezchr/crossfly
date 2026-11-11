//go:build crossfly_full

package jraider

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/df-mc/dragonfly/server/mob/jraid"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func init() {
	javamob.Register(javamob.Type{
		ID: RavagerID, Category: javamob.CategoryMonster, Width: 1.95, Height: 2.2, EyeHeight: 2.2 * 0.85,
		NotInPeaceful: true, XPReward: 20, LootTable: "entities/ravager",
		Families:   []string{"ravager", "monster", "mob"},
		Attributes: attributes("minecraft:ravager"),
		New:        func() javamob.Behaviour { return &Ravager{base: newBase(jraid.Ravager, "celebrate")} },
	})
}

// Ravager is the Ravager class. Not ported: the stun when its attack hits a raised shield (blockedByItem, a
// shield hook the base does not have) and its blindness while stunned or roaring (hasLineOfSight).
type Ravager struct {
	base
	st jraid.RavagerState
}

// RegisterGoals is Ravager.registerGoals.
func (r *Ravager) RegisterGoals(m *javamob.Mob) {
	r.registerRaiderGoals(m)
	m.SetPathfindingMalus(path.Leaves, 0)
	m.Goals().Add(0, goal.NewFloat(m))
	m.Goals().Add(4, goal.NewMeleeAttack(1, true))
	m.Goals().Add(5, goal.NewWaterAvoidingRandomStroll(0.4, 0.001))
	m.Goals().Add(6, goal.NewLookAtPlayer(6))
	m.Goals().Add(10, lookAtMobs(8))
	m.Targets().Add(2, hurtByTarget().SetAlertOthers())
	m.Targets().Add(3, goal.NewNearestAttackableTarget(nil, true, false))
	villagers := villagerTarget(true)
	villagers.Selector = func(e world.Entity, _ *world.Tx) bool { return !isBaby(e) }
	m.Targets().Add(4, villagers)
	m.Targets().Add(4, golemTarget(true))
}

func isBaby(e world.Entity) bool {
	b, ok := e.(interface{ Baby() bool })
	return ok && b.Baby()
}

// MaxHeadYRot is Ravager.getMaxHeadYRot.
func (r *Ravager) MaxHeadYRot() float64  { return 45 }
func (r *Ravager) MaxHeadXRot() float64  { return 40 }
func (r *Ravager) HeadRotSpeed() float64 { return 10 }

// ApplyRaidBuffs is Ravager.applyRaidBuffs: nothing.
func (r *Ravager) ApplyRaidBuffs(*javamob.Mob, int) {}

// PostTick is the part of Ravager.aiStep after super.aiStep: no movement while attacking, stunned or roaring, the
// speed easing to 0.35 with a target (0.3 without), leaves broken when it runs into them (mob griefing), and the
// attack, stun and roar timers.
func (r *Ravager) PostTick(m *javamob.Mob) {
	if m.Dead() {
		return
	}
	speed := m.Attributes().Instance(javamob.AttrMovementSpeed)
	_, hasTarget := m.Target()
	speed.SetBase(r.st.Speed(speed.Base(), r.st.Immobile(), hasTarget))
	if m.HorizontalCollision() && levelOf(m.Tx()).MobGriefing() {
		broke := false
		bb := m.BBox().Grow(0.2)
		lo, hi := bb.Min(), bb.Max()
		tx := m.Tx()
		for x := int(math.Floor(lo[0])); x <= int(math.Floor(hi[0])); x++ {
			for y := int(math.Floor(lo[1])); y <= int(math.Floor(hi[1])); y++ {
				for z := int(math.Floor(lo[2])); z <= int(math.Floor(hi[2])); z++ {
					pos := cube.Pos{x, y, z}
					if name, _ := tx.Block(pos).EncodeBlock(); len(name) > 7 && name[len(name)-7:] == "_leaves" {
						breakBlock(tx, pos, m)
						broke = true
					}
				}
			}
		}
		if !broke && m.OnGround() {
			m.JumpControl().Jump()
		}
	}
	roar, stunEnded := r.st.Tick()
	if roar {
		r.roar(m)
	}
	if stunEnded {
		m.PlaySound("roar")
	}
	if r.st.Immobile() {
		m.SetZza(0)
		m.SetXxa(0)
	}
}

// roar is Ravager.roar: 6 damage to living entities within 4 blocks but illagers and ravagers, and a strong
// push away for all but players.
func (r *Ravager) roar(m *javamob.Mob) {
	tx := m.Tx()
	for e := range tx.EntitiesWithin(m.BBox().Grow(4)) {
		if e.H() == m.H() || e.H().Type().EncodeEntity() == RavagerID || !alive(e) {
			continue
		}
		l, ok := e.(entity.Living)
		if !ok {
			continue
		}
		if !isIllager(e) {
			l.Hurt(jraid.RavagerRoarDamage, entity.AttackDamageSource{Attacker: m})
		}
		if !isPlayer(e) {
			p, q := e.Position(), m.Position()
			x, y, z := jraid.StrongKnockback(p[0]-q[0], p[2]-q[2])
			if jm, ok := javamob.Of(e); ok {
				jm.Push(mgl64.Vec3{x, y, z})
			} else if v, ok := e.(interface {
				Velocity() mgl64.Vec3
				SetVelocity(mgl64.Vec3)
			}); ok {
				v.SetVelocity(v.Velocity().Add(mgl64.Vec3{x, y, z}))
			}
		}
	}
	m.PlaySound("roar")
}

// DoHurtTarget is Ravager.doHurtTarget: the bite animation (entity event 4) and 10 ticks of standing still.
func (r *Ravager) DoHurtTarget(m *javamob.Mob, t world.Entity) bool {
	r.st.Attack()
	for _, v := range m.Tx().Viewers(m.Position()) {
		v.ViewEntityAction(m, kit.ActorEvent(packet.ActorEventStartAttacking))
	}
	return m.BaseDoHurtTarget(t)
}

func (r *Ravager) EncodeNBT(data map[string]any) {
	r.base.EncodeNBT(data)
	data["AttackTick"] = int32(r.st.AttackTick)
	data["StunTick"] = int32(r.st.StunnedTick)
	data["RoarTick"] = int32(r.st.RoarTick)
}

func (r *Ravager) DecodeNBT(m *javamob.Mob, data map[string]any) {
	r.base.DecodeNBT(m, data)
	get := func(k string) int {
		v, _ := data[k].(int32)
		return int(v)
	}
	r.st = jraid.RavagerState{AttackTick: get("AttackTick"), StunnedTick: get("StunTick"), RoarTick: get("RoarTick")}
}

// breakBlock is Level.destroyBlock(pos, true, mob): the block breaks with its drops.
func breakBlock(tx *world.Tx, pos cube.Pos, _ world.Entity) {
	b := tx.Block(pos)
	tx.SetBlock(pos, nil, nil)
	if br, ok := b.(block.Breakable); ok {
		for _, d := range br.BreakInfo().Drops(item.ToolNone{}, nil) {
			tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: pos.Vec3Centre()}, d))
		}
	}
}
