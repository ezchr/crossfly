package cmd

import (
	"github.com/df-mc/dragonfly/server/internal/sliceutil"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"slices"
)

// Target represents the target of a command. A []Target may be used as command parameter
// types to allow passing targets to the command.
type Target interface {
	// Position returns the position of the Target as an mgl64.Vec3.
	Position() mgl64.Vec3
}

// NamedTarget is a Target that has a name.
type NamedTarget interface {
	Target
	// Name returns a name of the Target. Note that this name needs not to be and is not unique for a Target.
	Name() string
}

// targets returns all Targets selectable by the Source passed.
func targets(tx *world.Tx) (entities []Target, players []NamedTarget) {
	if tx == nil {
		return nil, nil
	}
	ent := sliceutil.Convert[Target](slices.Collect(tx.Entities()))
	pl := sliceutil.Convert[NamedTarget](slices.Collect(tx.Players()))
	return ent, pl
}

// parseUUID parses a target given as a UUID in its usual 36 character form
// (8-4-4-4-12 hexadecimal digits), as Java Edition clients send for the entity
// they look at. A player name is never that long.
func parseUUID(s string) (uuid.UUID, bool) {
	if len(s) != 36 {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(s)
	return id, err == nil
}

// targetByUUID returns the Target in pool with the UUID passed: a player's
// UUID, or the UUID of an entity's handle.
func targetByUUID(pool []Target, id uuid.UUID) (Target, bool) {
	for _, t := range pool {
		var tid uuid.UUID
		switch v := t.(type) {
		case interface{ UUID() uuid.UUID }:
			tid = v.UUID()
		case world.Entity:
			tid = v.H().UUID()
		default:
			continue
		}
		if tid == id {
			return t, true
		}
	}
	return nil, false
}
