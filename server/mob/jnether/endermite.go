//go:build crossfly_full

package jnether

import (
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
)

// The endermite (Endermite, Paper 26.3): a small melee monster that
// despawns after 2400 ticks unless it is persistent; endermen attack it
// (their goal). Its spawn rule (no player within 5 unless from a spawner)
// is in the spawning package; ender pearls spawn it (entity package).

func init() {
	javamob.Register(javamob.Type{
		ID: "minecraft:endermite", Category: javamob.CategoryMonster,
		Width: 0.4, Height: 0.3, EyeHeight: 0.13,
		NotInPeaceful: true, Arthropod: true,
		Attributes: func() map[javamob.Attribute]float64 {
			a := javamob.MonsterAttributes()
			a[javamob.AttrMaxHealth] = 8
			a[javamob.AttrMovementSpeed] = 0.25
			a[javamob.AttrAttackDamage] = 2
			return a
		},
		Families: []string{"endermite", "arthropod", "monster", "mob"},
		XPReward: 3,
		New:      func() javamob.Behaviour { return &Endermite{} },
	})
}

// Endermite is the Endermite class.
type Endermite struct{ life int }

// RegisterGoals is Endermite.registerGoals.
func (e *Endermite) RegisterGoals(m *javamob.Mob) {
	m.Goals().Add(1, goal.NewFloat(m))
	// ClimbOnTopOfPowderSnowGoal(1): Dragonfly has no powder snow.
	m.Goals().Add(2, goal.NewMeleeAttack(1, false))
	m.Goals().Add(3, goal.NewWaterAvoidingRandomStroll(1, 0.001))
	m.Goals().Add(7, goal.NewLookAtPlayer(8))
	m.Goals().Add(8, goal.NewRandomLookAround())
	m.Targets().Add(1, goal.NewHurtByTarget().SetAlertOthers())
	m.Targets().Add(2, goal.NewNearestAttackableTarget(nil, true, false))
}

// Tick is Endermite.tick: the body turns with the head yaw before super.tick.
func (e *Endermite) Tick(m *javamob.Mob) { m.SetBodyYRot(m.YRot()) }

// PostTick is Endermite.aiStep after super (the end of the tick): the
// lifetime.
func (e *Endermite) PostTick(m *javamob.Mob) {
	if !m.PersistenceRequired() {
		e.life++
	}
	if e.life >= 2400 {
		_ = m.Close()
	}
}

// Persister: Lifetime.
func (e *Endermite) EncodeNBT(data map[string]any) { data["Lifetime"] = int32(e.life) }
func (e *Endermite) DecodeNBT(_ *javamob.Mob, data map[string]any) {
	if v, ok := data["Lifetime"].(int32); ok {
		e.life = int(v)
	}
}
