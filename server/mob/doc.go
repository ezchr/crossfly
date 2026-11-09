//go:build crossfly_full

// Package mob runs vanilla Bedrock mobs from their behaviour pack entity
// definitions (the minecraft:entity JSON files), the way BDS does.
//
// The package holds only the shared contracts: the Mob entity and its
// accessors, the registries every other mob package plugs into (components,
// goals, systems, hooks), the BDS goal selector and the fixed per-tick system
// order. The behaviour itself lives in sub-packages that each register what
// they implement from their own register.go; see CONTRACTS.md.
//
// A mob is opened like any Dragonfly entity: Type.Open wraps the entity's
// data in a *Mob that is only valid inside the transaction it was opened in.
// Everything a mob remembers between ticks lives in its state (the
// entity.Behaviour stored in world.EntityData.Data), never in the *Mob.
package mob
