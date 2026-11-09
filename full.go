//go:build crossfly_full

package main

import (
	"fmt"
	"log/slog"

	"github.com/df-mc/dragonfly/commands"
	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/java/javasession"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/spawn"
	"github.com/df-mc/dragonfly/server/mob/vanilla"
	"github.com/df-mc/dragonfly/server/world"
)

// Edition holds the settings of Crossfly Full's features in config.toml. Each can be turned off; a feature that
// is off is not set up at all, and costs nothing.
type Edition struct {
	Mobs          mobsConfig
	Redstone      redstoneConfig
	Capes         capesConfig
	ExtraCommands extraCommandsConfig
}

// mobsConfig holds the settings of the mobs.
type mobsConfig struct {
	// Behaviour is how mobs behave: "bedrock" runs the vanilla mobs with the AI of Bedrock Edition (its behaviour
	// packs), "off" has no mobs.
	Behaviour string `comment:"Behaviour is how mobs behave: bedrock runs the vanilla mobs with the AI of Bedrock Edition (from its behaviour packs), off has no mobs."`
	// NaturalSpawning spawns mobs around players as in vanilla, by the spawn rules of Bedrock Edition.
	NaturalSpawning bool `comment:"NaturalSpawning spawns mobs around players as in vanilla, by the spawn rules of Bedrock Edition."`
}

// redstoneConfig holds the settings of redstone.
type redstoneConfig struct {
	// Behaviour is how redstone works: "java" as in Java Edition, "dragonfly" is Dragonfly's own redstone.
	Behaviour string `comment:"Behaviour is how redstone works: java works as in Java Edition (its tick order and update rules; pistons, observers, repeaters, comparators, dispensers, droppers, rails, minecarts, lamps, buttons and pressure plates; liquids and TNT as in Java Edition), dragonfly is Dragonfly's own redstone (dust, torches, levers and TNT)."`
	// AllowPistonDuplication lets pistons duplicate TNT, rails and carpets as in vanilla Java Edition.
	AllowPistonDuplication bool `comment:"AllowPistonDuplication lets pistons duplicate TNT, rails and carpets as in vanilla Java Edition. Off, as on Paper servers. Only with Behaviour java."`
	// EntitiesCreatePortals lets entities other than players create nether portals, as in vanilla Java Edition.
	EntitiesCreatePortals bool `comment:"EntitiesCreatePortals lets entities other than players create nether portals as in vanilla Java Edition (TNT sent through a portal). Off, only players create them. Only with Behaviour java."`
}

// capesConfig holds the settings of capes.
type capesConfig struct {
	// JavaCapes shows the capes of Java Edition players to Bedrock players.
	JavaCapes bool `comment:"JavaCapes shows the capes of Java Edition players (their Mojang capes) to Bedrock players."`
}

// extraCommandsConfig holds the settings of the commands beyond the basic vanilla ones.
type extraCommandsConfig struct {
	// Enabled registers /setblock and /fill, and with the mobs on /summon and /mobdebug.
	Enabled bool `comment:"Enabled registers /setblock and /fill, and with the mobs on /summon and /mobdebug (for operators)."`
}

func defaultEdition() Edition {
	return Edition{
		Mobs:          mobsConfig{Behaviour: "bedrock", NaturalSpawning: true},
		Redstone:      redstoneConfig{Behaviour: "java"},
		Capes:         capesConfig{JavaCapes: true},
		ExtraCommands: extraCommandsConfig{Enabled: true},
	}
}

func (e Edition) mobs() bool         { return e.Mobs.Behaviour == "bedrock" }
func (e Edition) javaRedstone() bool { return e.Redstone.Behaviour == "java" }

// register sets up the features that are on and registers their blocks, items and entity types. It runs before
// the server is configured, which makes the block registry and the creative inventory.
func (e Edition) register(log *slog.Logger) error {
	switch e.Mobs.Behaviour {
	case "bedrock", "off":
	default:
		return fmt.Errorf("Mobs.Behaviour %q is not \"bedrock\" or \"off\"", e.Mobs.Behaviour)
	}
	switch e.Redstone.Behaviour {
	case "java", "dragonfly":
	default:
		return fmt.Errorf("Redstone.Behaviour %q is not \"java\" or \"dragonfly\"", e.Redstone.Behaviour)
	}
	world.SetJavaRedstone(e.javaRedstone())
	if e.javaRedstone() {
		block.RegisterJavaRedstone()
		item.RegisterJavaRedstone()
	}
	if e.mobs() {
		entity.SetMobs(true)
		item.RegisterMobItems()
		existing := map[string]bool{}
		for _, t := range entity.DefaultRegistry.Types() {
			existing[t.EncodeEntity()] = true
		}
		if _, err := vanilla.Register(vanilla.Options{Skip: func(id string) bool { return existing[id] }}); err != nil {
			return fmt.Errorf("register mobs: %w", err)
		}
	}
	javasession.SetCapes(e.Capes.JavaCapes)
	log.Info("Crossfly Full.", "mobs", e.Mobs.Behaviour, "redstone", e.Redstone.Behaviour, "java-capes", e.Capes.JavaCapes, "extra-commands", e.ExtraCommands.Enabled)
	return nil
}

// configure adds the entity types of the features that are on to the server's entity registry.
func (e Edition) configure(conf *server.Config) {
	c := entity.DefaultRegistry.Config()
	types := entity.DefaultRegistry.Types()
	if e.javaRedstone() {
		c = entity.WithJavaRedstone(c)
		types = append(types, entity.JavaRedstoneTypes()...)
		conf.AllowPistonDuplication = e.Redstone.AllowPistonDuplication
		conf.EntitiesCreatePortals = e.Redstone.EntitiesCreatePortals
	}
	if e.mobs() {
		types = append(types, mob.Types()...)
	}
	conf.Entities = c.New(types)
}

// start turns on natural spawning and registers the extra commands, once the server runs.
func (e Edition) start(srv *server.Server, _ *slog.Logger) {
	if e.mobs() && e.Mobs.NaturalSpawning {
		for _, w := range []*world.World{srv.World(), srv.Nether(), srv.End()} {
			spawn.Enable(w, spawn.Config{})
		}
	}
	if e.ExtraCommands.Enabled {
		commands.RegisterExtra()
	}
}

// firstStart does nothing in Crossfly Full.
func firstStart(*slog.Logger) {}
