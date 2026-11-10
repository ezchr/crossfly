//go:build crossfly_full

package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/df-mc/dragonfly/commands"
	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/java/javasession"
	"github.com/df-mc/dragonfly/server/mob"
	javamobs "github.com/df-mc/dragonfly/server/mob/javamob/vanilla"
	"github.com/df-mc/dragonfly/server/mob/jnether"
	"github.com/df-mc/dragonfly/server/mob/spawn"
	"github.com/df-mc/dragonfly/server/mob/vanilla"
	"github.com/df-mc/dragonfly/server/world"
)

// Edition holds the settings of Crossfly Full's features in config.toml: the commands with their operators and the
// console, the flat world, and the mobs, Java redstone, capes and extra commands, each of which can be turned off;
// a feature that is off is not set up at all, and costs nothing.
type Edition struct {
	Commands      commandsConfig
	FlatWorld     flatConfig
	Mobs          mobsConfig
	Redstone      redstoneConfig
	Capes         capesConfig
	ExtraCommands extraCommandsConfig
}

// commandsConfig holds the settings of the commands.
type commandsConfig struct {
	// OperatorsFile is the JSON file with the players who may run every command (/op, /deop). The server
	// console runs every command too: type "op <name>" there to add the first one.
	OperatorsFile string `comment:"OperatorsFile is the JSON file of the players who may run every command (/op, /deop). The server console runs every command too: type op <name> there to add the first operator."`
}

// mobsConfig holds the settings of the mobs.
type mobsConfig struct {
	// Behaviour is how mobs behave: "bedrock" runs the vanilla mobs with the AI of Bedrock Edition (its behaviour
	// packs); "java" runs them as on a Paper server (Java Edition 26.3 with Paper's default config: goals,
	// pathfinding, physics and drops as in Java Edition, so Java mob farms work; mobs not ported to Java yet keep
	// Bedrock's AI); "off" has no mobs. The environment variable DF_MOBS overrides it.
	Behaviour string `comment:"Behaviour is how mobs behave: bedrock runs the vanilla mobs with the AI of Bedrock Edition (from its behaviour packs), java runs them as on a Paper server (Java Edition AI, pathfinding and physics, so Java mob farms work; mobs not ported to Java yet keep the Bedrock AI), off has no mobs. The environment variable DF_MOBS overrides it."`
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
		Commands:      commandsConfig{OperatorsFile: "ops.json"},
		FlatWorld:     defaultFlat(),
		Mobs:          mobsConfig{Behaviour: "bedrock", NaturalSpawning: true},
		Redstone:      redstoneConfig{Behaviour: "java"},
		Capes:         capesConfig{JavaCapes: true},
		ExtraCommands: extraCommandsConfig{Enabled: true},
	}
}

func (e Edition) mobs() bool         { return e.Mobs.Behaviour == "bedrock" || e.javaMobs() }
func (e Edition) javaMobs() bool     { return e.Mobs.Behaviour == "java" }
func (e Edition) javaRedstone() bool { return e.Redstone.Behaviour == "java" }

// register sets up the features that are on and registers their blocks, items and entity types. It runs before
// the server is configured, which makes the block registry and the creative inventory.
func (e *Edition) register(log *slog.Logger) error {
	if v := os.Getenv("DF_MOBS"); v != "" {
		e.Mobs.Behaviour = v
	}
	switch e.Mobs.Behaviour {
	case "bedrock", "java", "off":
	default:
		return fmt.Errorf("Mobs.Behaviour %q is not \"bedrock\", \"java\" or \"off\"", e.Mobs.Behaviour)
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
		block.RegisterMobBlocks()
		item.RegisterMobItems()
		existing := map[string]bool{}
		for _, t := range entity.DefaultRegistry.Types() {
			existing[t.EncodeEntity()] = true
		}
		if _, err := vanilla.Register(vanilla.Options{Skip: func(id string) bool { return existing[id] }}); err != nil {
			return fmt.Errorf("register mobs: %w", err)
		}
		if e.javaMobs() {
			// After the Bedrock types: the Java types replace the ones they port.
			javamobs.Install()
			// Zombified piglins from nether portals (package mob/jnether).
			jnether.Install()
		}
		item.RegisterSpawnEggs(mob.Identifiers(), mob.EggSpawner{})
	}
	javasession.SetCapes(e.Capes.JavaCapes)
	log.Info("Crossfly Full.", "mobs", e.Mobs.Behaviour, "redstone", e.Redstone.Behaviour, "java-capes", e.Capes.JavaCapes, "extra-commands", e.ExtraCommands.Enabled)
	return nil
}

// operators are the operators of ops.json (Commands.OperatorsFile), loaded by Edition.configure.
var operators *commands.Operators

// configure sets the flat overworld generator, loads the operators and adds the entity types of the features that
// are on to the server's entity registry.
func (e Edition) configure(conf *server.Config) error {
	if err := e.FlatWorld.apply(conf); err != nil {
		return err
	}
	ops, err := commands.LoadOperators(e.Commands.OperatorsFile)
	if err != nil {
		return fmt.Errorf("operators %s: %w", e.Commands.OperatorsFile, err)
	}
	operators = ops
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
	return nil
}

// start registers the commands, turns on natural spawning and reads commands from the console (standard input),
// once the server runs.
func (e Edition) start(srv *server.Server, log *slog.Logger) {
	commands.Register(srv, operators)
	// Java mob mode has its own natural spawning (package mob/javaspawn hooks in here).
	if e.mobs() && !e.javaMobs() && e.Mobs.NaturalSpawning {
		for _, w := range []*world.World{srv.World(), srv.Nether(), srv.End()} {
			spawn.Enable(w, spawn.Config{})
		}
	}
	if e.ExtraCommands.Enabled {
		commands.RegisterExtra()
	}
	go commands.RunConsole(os.Stdin, log)
}

// firstStart does nothing in Crossfly Full.
func firstStart(*slog.Logger) {}
