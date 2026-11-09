//go:build crossfly_full

// Package vanilla links in every mob package and registers the vanilla
// Bedrock mobs from the embedded definitions.
//
// Import it (it is the only import a server needs) and add Types to the
// server's entity registry:
//
//	types, err := vanilla.Register(vanilla.Options{Skip: alreadyRegistered})
package vanilla

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/loot"
	"github.com/df-mc/dragonfly/server/mob/component/village"
	"github.com/df-mc/dragonfly/server/mob/data"
	"github.com/df-mc/dragonfly/server/mob/spawn"
	// Phase 1 packages. They register themselves from their own register.go
	// (init); nothing else here changes.
	_ "github.com/df-mc/dragonfly/server/mob/component/combat" // E hostile components
	_ "github.com/df-mc/dragonfly/server/mob/component/core"   // D passive components
	_ "github.com/df-mc/dragonfly/server/mob/control"          // C move/look/jump/body controls, movement.*, jump.*
	_ "github.com/df-mc/dragonfly/server/mob/def"              // A1 definitions, groups, properties, events
	_ "github.com/df-mc/dragonfly/server/mob/filter"           // A2 filters
	_ "github.com/df-mc/dragonfly/server/mob/goal/boss"        // bosses: dragon, wither, warden (+ component/boss)
	_ "github.com/df-mc/dragonfly/server/mob/goal/combat"      // E hostile goals
	_ "github.com/df-mc/dragonfly/server/mob/goal/core"        // D passive goals
	_ "github.com/df-mc/dragonfly/server/mob/goal/equip"       // charge_held_item (crossbow charging)
	_ "github.com/df-mc/dragonfly/server/mob/goal/locomotion"  // water/flying/slime/squid movement (+ component/locomotion)
	_ "github.com/df-mc/dragonfly/server/mob/goal/misc"        // misc goals (+ component/misc)
	_ "github.com/df-mc/dragonfly/server/mob/goal/ride"        // riding, taming, owners (+ component/ride, component/tame)
	_ "github.com/df-mc/dragonfly/server/mob/goal/village"     // villagers, traders, raids, golems, piglins (+ component/village)
	_ "github.com/df-mc/dragonfly/server/mob/nav"              // B pathfinder + navigation.*
	_ "github.com/df-mc/dragonfly/server/mob/physics"          // C travel, knock back, fall damage
)

// Definitions parses every embedded entity definition with the installed
// parser (package mob/def once linked, the minimal reader before).
func Definitions() ([]mob.Definition, error) {
	files := data.Entities()
	out := make([]mob.Definition, 0, len(files))
	for _, f := range files {
		d, err := mob.ParseDefinition(f.Data)
		if err != nil {
			return nil, fmt.Errorf("vanilla: %v: %w", f.Name, err)
		}
		out = append(out, d)
	}
	return out, nil
}

// Options control Register.
type Options struct {
	// Skip leaves out identifiers that are implemented elsewhere (Dragonfly's
	// own arrow, item, xp_orb, tnt, ... types). minecraft:player is always
	// skipped.
	Skip func(id string) bool
	// All registers every definition, not only mobs (definitions with at
	// least one minecraft:behavior.* component).
	All bool
}

// Register registers the vanilla mobs as entity types and returns them, to
// be added to the server's entity registry. It also registers what the mobs
// need beside them (the items, enchantment and effects Dragonfly lacks, the
// tick hooks of natural spawning and villages), so call it before the server
// is configured. Nothing of the mobs runs until it is called.
func Register(opts Options) ([]*mob.Type, error) {
	loot.Enable()
	village.Enable()
	spawn.RegisterHook()
	defs, err := Definitions()
	if err != nil {
		return nil, err
	}
	var out []*mob.Type
	for _, d := range defs {
		id := d.Identifier()
		if id == "minecraft:player" || (opts.Skip != nil && opts.Skip(id)) {
			continue
		}
		if !opts.All && !IsMob(d) {
			continue
		}
		out = append(out, mob.RegisterType(d))
	}
	return out, nil
}

// IsMob reports whether a definition has goals anywhere.
func IsMob(d mob.Definition) bool {
	for _, k := range d.ComponentKeys() {
		if strings.HasPrefix(k, "minecraft:behavior.") {
			return true
		}
	}
	return false
}
