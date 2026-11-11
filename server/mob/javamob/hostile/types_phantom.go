//go:build crossfly_full

package hostile

import (
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/go-gl/mathgl/mgl64"
)

func init() {
	javamob.Register(javamob.Type{
		ID: phantomID, Category: javamob.CategoryMonster, Width: 0.9, Height: 0.5, EyeHeight: 0.175,
		NotInPeaceful: true, Undead: true, BurnsInDaylight: true, XPReward: PhantomXP,
		Families:   []string{"phantom", "undead", "monster", "mob"},
		Attributes: javamob.MonsterAttributes,
		New:        func() javamob.Behaviour { return &PhantomMob{} },
	})
}

// PhantomMob is Phantom on the base: PhantomMoveControl,
// PhantomLookControl, PhantomBodyRotationControl, travelFlying and the
// goals of phantom.go. The per-mob state is the Phantom of phantom.go.
type PhantomMob struct{ p *Phantom }

// RegisterGoals is Phantom.registerGoals and the controls of its
// constructor.
func (b *PhantomMob) RegisterGoals(m *javamob.Mob) {
	b.p = phantomOf(m.Mob)
	m.MoveControl().Custom = func(m *javamob.Mob, _ *javamob.MoveControl) { phantomMoveControl{}.Tick(m.Mob) }
	m.LookControl().Custom = func(*javamob.Mob, *javamob.LookControl) {}
	m.BodyRotationControl().Custom = func(m *javamob.Mob, _ *javamob.BodyRotationControl) { phantomBodyControl{}.Tick(m.Mob) }
	m.Goals().Add(1, goalOf(&phantomStrategyGoal{}))
	m.Goals().Add(2, goalOf(&phantomSweepGoal{}))
	m.Goals().Add(3, goalOf(&phantomCircleGoal{}))
	m.Targets().Add(1, goalOf(newPhantomTargetGoal()))
}

// FinalizeSpawn is Phantom.finalizeSpawn.
func (b *PhantomMob) FinalizeSpawn(m *javamob.Mob, _ javamob.SpawnReason) {
	PhantomFinalizeSpawn(m.Mob)
}

// Travel is Phantom.travel: travelFlying(input, 0.2), no gravity.
func (b *PhantomMob) Travel(m *javamob.Mob, input mgl64.Vec3) bool {
	PhantomTravel(m.Mob, input)
	return true
}

// EncodeNBT and DecodeNBT save the anchor and the size.
func (b *PhantomMob) EncodeNBT(data map[string]any)                 { b.p.EncodeNBT(data) }
func (b *PhantomMob) DecodeNBT(m *javamob.Mob, data map[string]any) { b.p.DecodeNBT(m.Mob, data) }
