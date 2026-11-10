//go:build crossfly_full

// Package jmob connects the Java Brain framework (server/mob/jbrain) to the
// Java mob base (server/mob/javamob) and holds the sensors and behaviours
// several brain-driven mobs share (villagers, piglins, hoglins, wardens,
// allays, frogs, goats, axolotls, camels, sniffers, armadillos).
//
// Brains of Java mobs have the body type *javamob.Mob: a behaviour reaches
// its mob's own state with m.Behaviour().(*MyMob). Build a jbrain.Provider
// per mob type and call Attach from the mob's RegisterGoals (or after
// loading): the base ticks the brain where Java's Mob.customServerAiStep
// does (javamob.Brain).
//
//	var provider = &jbrain.Provider[*javamob.Mob]{
//		EntityType: "piglin",
//		Sensors:    []*jbrain.SensorType[*javamob.Mob]{jmob.NearestLivingEntities, jmob.NearestPlayers, ...},
//		Activities: func(m *javamob.Mob) []jbrain.ActivityData[*javamob.Mob] { ... },
//	}
//	b := jmob.Attach(m, provider, saved)
//
// Every function here cites the Java class it rewrites.
package jmob

import (
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/jbrain"
)

// Brain is a brain of a Java mob.
type Brain = jbrain.Brain[*javamob.Mob]

// Control is a behaviour of a Java mob's brain.
type Control = jbrain.Control[*javamob.Mob]

// ticker adapts a Brain to javamob.Brain.
type ticker struct{ b *Brain }

// Tick runs Brain.tick with the mob's transaction, the game time
// (Level.getGameTime) and its random.
func (t ticker) Tick(m *javamob.Mob) { t.b.Tick(Ctx(m), m) }

// Ctx returns the brain context for m (without memories; Brain.Tick and
// Brain.Ctx fill them in).
func Ctx(m *javamob.Mob) jbrain.Ctx {
	tx := m.Tx()
	return jbrain.Ctx{Tx: tx, Time: tx.CurrentTick(), Rand: m.Rand()}
}

// Attach makes a brain for m from p (Brain.Provider.makeBrain) and gives it
// to the mob (m.SetBrain). saved are the memories read from NBT, or nil.
func Attach(m *javamob.Mob, p *jbrain.Provider[*javamob.Mob], saved *jbrain.Packed) *Brain {
	b := p.MakeBrain(m, m.Rand(), saved)
	m.SetBrain(ticker{b})
	return b
}

// Of returns the jbrain brain of m, if it has one attached by Attach.
func Of(m *javamob.Mob) (*Brain, bool) {
	t, ok := m.Brain().(ticker)
	if !ok {
		return nil, false
	}
	return t.b, true
}

// Mem returns the memories of m's brain, or nil.
func Mem(m *javamob.Mob) *jbrain.Memories {
	if b, ok := Of(m); ok {
		return b.Mem()
	}
	return nil
}

// RunCtx returns a context bound to m's brain for calling behaviours outside
// the brain tick (interaction handlers, other mobs' behaviours).
func RunCtx(m *javamob.Mob) *jbrain.Ctx {
	c := Ctx(m)
	if b, ok := Of(m); ok {
		return b.Ctx(c)
	}
	return &c
}
