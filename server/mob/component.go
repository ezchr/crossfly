//go:build crossfly_full

package mob

import (
	"encoding/json"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Component is a non-goal entity component (minecraft:health,
// minecraft:navigation.walk, minecraft:timer, ...). A new Component is built
// from its JSON each time the key becomes active on a mob (or its JSON
// changes), attached, and detached when it goes away again, so it may keep
// per-mob state.
type Component interface {
	// Attach is called once when the component becomes active on m.
	Attach(m *Mob)
	// Detach is called once when the component stops being active on m (a
	// group removed it or replaced its JSON). It is not called when the mob
	// dies, is removed or unloads.
	Detach(m *Mob)
}

// ComponentFactory builds a component from its JSON. key is the full JSON
// key, so one factory may serve several keys (navigation.walk/climb/...).
type ComponentFactory func(key string, raw json.RawMessage) (Component, error)

// Optional component hooks. A component implements any of these to take part
// in the matching part of the mob's life. They are called in definition order.
type (
	// Ticker components are ticked once per tick in the stage TickStage
	// returns (StagedTicker), or in StageSensors when they don't say.
	Ticker interface{ Tick(m *Mob) }
	// FrozenTicker components keep doing something while their mob is
	// beyond the simulation distance of every player and does not tick (a
	// boss bar hidden from players that moved away). TickFrozen runs
	// instead of the mob's tick; it must not move the mob or change what it
	// does.
	FrozenTicker interface{ TickFrozen(m *Mob) }
	// StagedTicker is a Ticker that picks its stage.
	StagedTicker interface {
		Ticker
		TickStage() Stage
	}
	// DamageModifier components see damage before it is applied
	// (damage_sensor: cancel, multiply, fire events).
	DamageModifier interface{ ModifyDamage(m *Mob, d *Damage) }
	// HurtListener components hear about damage that was dealt.
	HurtListener interface {
		OnHurt(m *Mob, dealt float64, src world.DamageSource)
	}
	// DeathListener components run when m dies (loot, experience_reward,
	// on_death, transformation into a drop).
	DeathListener interface {
		OnDeath(m *Mob, src world.DamageSource)
	}
	// Interactor components handle a player using the mob (interact, milk,
	// shear, breed, tame, ...). The first that reports true wins.
	Interactor interface {
		Interact(m *Mob, user item.User, held item.Stack, clickPos mgl64.Vec3) (item.Stack, bool)
	}
	// RemoveListener components run when m is removed from the world for
	// good with Close (despawn, /kill-style removal), not when it dies or
	// its chunk unloads: a boss hides its bar.
	RemoveListener interface{ OnRemove(m *Mob) }
	// TargetListener components hear about target changes
	// (on_target_acquired / on_target_escape).
	TargetListener interface {
		OnTargetChanged(m *Mob, old, new world.Entity)
	}
	// Persister components save state with the mob. Their map is stored
	// under the component key. EncodeNBT runs outside any transaction (chunk
	// saving), so it gets no mob: encode the component's own fields only.
	// DecodeNBT runs after the component attached on load.
	Persister interface {
		EncodeNBT() map[string]any
		DecodeNBT(m *Mob, data map[string]any)
	}
)

// Damage is damage about to be dealt to a mob, passed through the
// DamageModifier components.
type Damage struct {
	Amount float64
	Source world.DamageSource
	// Attacker is the entity responsible (the attacker, or a projectile's
	// owner), or nil.
	Attacker world.Entity
	// Cancel drops the damage. A cancelled hit still reports HurtIgnored.
	Cancel bool
	// NoHurtAnimation suppresses the hurt animation and sound.
	NoHurtAnimation bool
}

// attachedComponent is one active component of a mob.
type attachedComponent struct {
	key  string
	raw  json.RawMessage
	comp Component // nil for goals and unsupported keys
	goal bool
}
