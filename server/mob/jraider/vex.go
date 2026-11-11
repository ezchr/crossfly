//go:build crossfly_full

package jraider

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

func init() {
	javamob.Register(javamob.Type{
		ID: VexID, Category: javamob.CategoryMonster, Width: 0.4, Height: 0.8, EyeHeight: 0.51875,
		NotInPeaceful: true, FireImmune: true, XPReward: 3, LootTable: "entities/vex",
		Families:   []string{"vex", "monster", "mob"},
		Attributes: attributes("minecraft:vex"),
		New:        func() javamob.Behaviour { return &Vex{} },
	})
}

func alive(e world.Entity) bool {
	l, ok := e.(entity.Living)
	return !ok || !l.Dead()
}

// Vex is the Vex class. Vexes fly with their own move control (VexMoveControl) and no gravity. Not ported: they
// pass through blocks in Java (noPhysics); the Java mob base has no noPhysics travel, so Crossfly vexes collide.
type Vex struct {
	owner   *world.EntityHandle
	bound   *cube.Pos
	limited bool
	life    int

	// The VexMoveControl: wanted position and speed while moving.
	moving   bool
	wanted   mgl64.Vec3
	speed    float64
	charging bool
}

// RegisterGoals is Vex.registerGoals.
func (v *Vex) RegisterGoals(m *javamob.Mob) {
	m.Goals().Add(0, goal.NewFloat(m))
	m.Goals().Add(4, &vexCharge{GoalBase: javamob.GoalBase{Control: javamob.FlagMove}, v: v})
	m.Goals().Add(8, &vexRandomMove{GoalBase: javamob.GoalBase{Control: javamob.FlagMove}, v: v})
	look := goal.NewLookAtPlayer(3)
	look.Probability = 1
	m.Goals().Add(9, look)
	m.Goals().Add(10, lookAtMobs(8))
	m.Targets().Add(1, hurtByTarget().SetAlertOthers())
	m.Targets().Add(2, &vexCopyOwnerTarget{v: v})
	m.Targets().Add(3, goal.NewNearestAttackableTarget(nil, true, false))
}

// FinalizeSpawn is Vex.finalizeSpawn: an iron sword that never drops.
func (v *Vex) FinalizeSpawn(m *javamob.Mob, _ javamob.SpawnReason) {
	m.SetItemSlot(javamob.SlotMainHand, item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1))
	m.SetDropChance(javamob.SlotMainHand, 0)
}

// Tick is Vex.tick: no gravity; a vex with limited life starves once it ran out, 1 damage a second.
func (v *Vex) Tick(m *javamob.Mob) {
	if !m.Meta().NoGravity {
		m.UpdateMeta(func(meta *mob.Meta) { meta.NoGravity = true })
	}
	if v.limited {
		if v.life--; v.life <= 0 {
			v.life = 20
			m.Hurt(1, player.StarvationDamageSource{})
		}
	}
}

// CustomServerAiStep is VexMoveControl.tick (the base move control stays idle: the vex sets no wanted position on
// it).
func (v *Vex) CustomServerAiStep(m *javamob.Mob) {
	if !v.moving {
		return
	}
	p := m.Position()
	d := v.wanted.Sub(p)
	l := d.Len()
	bb := m.BBox()
	size := (bb.Width() + bb.Height() + bb.Length()) / 3
	if l < size {
		v.moving = false
		m.SetVelocity(m.Velocity().Mul(0.5))
		return
	}
	vel := m.Velocity().Add(d.Mul(v.speed * 0.05 / l))
	m.SetVelocity(vel)
	var yaw float64
	if t, ok := m.Target(); ok {
		tp := t.Position()
		yaw = -math.Atan2(tp[0]-p[0], tp[2]-p[2]) * 180 / math.Pi
	} else {
		yaw = -math.Atan2(vel[0], vel[2]) * 180 / math.Pi
	}
	m.SetYRot(yaw)
	m.SetBodyYRot(yaw)
}

func (v *Vex) setWanted(p mgl64.Vec3, speed float64) { v.moving, v.wanted, v.speed = true, p, speed }

func (v *Vex) setCharging(m *javamob.Mob, on bool) {
	if v.charging != on {
		v.charging = on
		m.SetActorFlag(protocol.EntityDataFlagCharging, on)
	}
}

// SensitiveToWater: no. LightLevelDependentMagicValue is 1 in Java (always bright); nothing reads it here.

// EncodeNBT is Vex.addAdditionalSaveData.
func (v *Vex) EncodeNBT(data map[string]any) {
	if v.bound != nil {
		data["bound_pos"] = []int32{int32(v.bound[0]), int32(v.bound[1]), int32(v.bound[2])}
	}
	if v.limited {
		data["life_ticks"] = int32(v.life)
	}
}

// DecodeNBT is Vex.readAdditionalSaveData (the owner is not kept across saves).
func (v *Vex) DecodeNBT(_ *javamob.Mob, data map[string]any) {
	if b, ok := data["bound_pos"].([]int32); ok && len(b) == 3 {
		p := cube.Pos{int(b[0]), int(b[1]), int(b[2])}
		v.bound = &p
	}
	if l, ok := data["life_ticks"].(int32); ok {
		v.limited, v.life = true, int(l)
	}
}

func eyeOf(e world.Entity) mgl64.Vec3 {
	if l, ok := e.(interface{ EyeHeight() float64 }); ok {
		return e.Position().Add(mgl64.Vec3{0, l.EyeHeight()})
	}
	return e.Position()
}

// vexCharge is Vex.VexChargeAttackGoal.
type vexCharge struct {
	javamob.GoalBase
	v *Vex
}

func (g *vexCharge) CanUse(m *javamob.Mob) bool {
	t, ok := m.Target()
	if !ok || !alive(t) || g.v.moving || m.Rand().IntN(javamob.ReducedTickDelay(7)) != 0 {
		return false
	}
	return t.Position().Sub(m.Position()).LenSqr() > 4
}

func (g *vexCharge) CanContinueToUse(m *javamob.Mob) bool {
	t, ok := m.Target()
	return g.v.moving && g.v.charging && ok && alive(t)
}

func (g *vexCharge) Start(m *javamob.Mob) {
	if t, ok := m.Target(); ok {
		g.v.setWanted(eyeOf(t), 1)
	}
	g.v.setCharging(m, true)
	m.PlaySound("charge")
}

func (g *vexCharge) Stop(m *javamob.Mob)           { g.v.setCharging(m, false) }
func (g *vexCharge) RequiresUpdateEveryTick() bool { return true }

func (g *vexCharge) Tick(m *javamob.Mob) {
	t, ok := m.Target()
	if !ok {
		return
	}
	if m.BBox().IntersectsWith(t.H().Type().BBox(t).Translate(t.Position())) {
		m.DoHurtTarget(t)
		g.v.setCharging(m, false)
		return
	}
	if t.Position().Sub(m.Position()).LenSqr() < 9 {
		g.v.setWanted(eyeOf(t), 1)
	}
}

// vexRandomMove is Vex.VexRandomMoveGoal.
type vexRandomMove struct {
	javamob.GoalBase
	v *Vex
}

func (g *vexRandomMove) CanUse(m *javamob.Mob) bool {
	return !g.v.moving && m.Rand().IntN(javamob.ReducedTickDelay(7)) == 0
}
func (g *vexRandomMove) CanContinueToUse(*javamob.Mob) bool { return false }

func (g *vexRandomMove) Tick(m *javamob.Mob) {
	origin := cube.PosFromVec3(m.Position())
	if g.v.bound != nil {
		origin = *g.v.bound
	}
	r := m.Rand()
	for i := 0; i < 3; i++ {
		p := origin.Add(cube.Pos{r.IntN(15) - 7, r.IntN(11) - 5, r.IntN(15) - 7})
		if name, _ := m.Tx().Block(p).EncodeBlock(); name == "minecraft:air" {
			c := mgl64.Vec3{float64(p[0]) + 0.5, float64(p[1]) + 0.5, float64(p[2]) + 0.5}
			g.v.setWanted(c, 0.25)
			if _, ok := m.Target(); !ok {
				m.LookControl().SetLookAtWith(c, 180, 20)
			}
			return
		}
	}
}

// Start is unused: the goal works in its first tick.
func (g *vexRandomMove) RequiresUpdateEveryTick() bool { return true }

// vexCopyOwnerTarget is Vex.VexCopyOwnerTargetGoal: the vex attacks what its evoker attacks.
type vexCopyOwnerTarget struct {
	javamob.GoalBase
	v *Vex
	t *world.EntityHandle
}

func (g *vexCopyOwnerTarget) ownerTarget(m *javamob.Mob) (world.Entity, bool) {
	if g.v.owner == nil {
		return nil, false
	}
	o, ok := javamob.OfHandle(m.Tx(), g.v.owner)
	if !ok {
		return nil, false
	}
	return o.Target()
}

func (g *vexCopyOwnerTarget) CanUse(m *javamob.Mob) bool {
	t, ok := g.ownerTarget(m)
	return ok && alive(t)
}

func (g *vexCopyOwnerTarget) CanContinueToUse(m *javamob.Mob) bool {
	t, ok := m.Target()
	return ok && alive(t)
}

func (g *vexCopyOwnerTarget) Start(m *javamob.Mob) {
	if t, ok := g.ownerTarget(m); ok {
		m.SetTarget(t)
	}
}

func (g *vexCopyOwnerTarget) Stop(m *javamob.Mob) { m.SetTarget(nil) }
