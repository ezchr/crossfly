//go:build crossfly_full

package main

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/java/javamap"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/generator"
)

// flatConfig sets up the flat overworld new chunks are generated with.
type flatConfig struct {
	// Layers are the blocks of the flat overworld from the top down, by item name ("minecraft:" may be left out).
	Layers []string `comment:"Layers are the blocks of the flat overworld from the top down, by item name (minecraft: may be left out), e.g. [\"snow_layer\", \"grass_block\", \"dirt\", \"dirt\", \"bedrock\"]. Only new chunks use them."`
	// Biome is the biome of the flat overworld, by Bedrock or Java name, e.g. plains or snowy_plains.
	Biome string `comment:"Biome is the biome of the flat overworld, by Bedrock or Java name, e.g. plains, ice_plains or snowy_plains."`
}

func defaultFlat() flatConfig {
	return flatConfig{Layers: []string{"grass_block", "dirt", "dirt", "bedrock"}, Biome: "plains"}
}

// apply sets the overworld generator of conf. The nether and the end keep their flat defaults.
func (f flatConfig) apply(conf *server.Config) error {
	layers := make([]world.Block, 0, len(f.Layers))
	for _, name := range f.Layers {
		if !strings.Contains(name, ":") {
			name = "minecraft:" + name
		}
		it, ok := world.ItemByName(name, 0)
		if !ok {
			return fmt.Errorf("flat world: unknown block %q", name)
		}
		b, ok := it.(world.Block)
		if !ok {
			return fmt.Errorf("flat world: %q is not a block", name)
		}
		layers = append(layers, b)
	}
	if len(layers) == 0 {
		return fmt.Errorf("flat world: no layers")
	}
	bi, ok := biomeByName(f.Biome)
	if !ok {
		return fmt.Errorf("flat world: unknown biome %q", f.Biome)
	}
	// The generator is made when the server asks for it: generator.NewFlat looks up block runtime
	// IDs, which only exist once the block registry is finalised, after the config is read.
	conf.Generator = func(dim world.Dimension) world.Generator {
		switch dim {
		case world.Nether:
			return generator.NewFlat(biome.NetherWastes{}, []world.Block{block.Netherrack{}, block.Netherrack{}, block.Netherrack{}, block.Bedrock{}})
		case world.End:
			return generator.NewFlat(biome.End{}, []world.Block{block.EndStone{}, block.EndStone{}, block.EndStone{}, block.Bedrock{}})
		}
		return generator.NewFlat(bi, layers)
	}
	return nil
}

// biomeByName finds a biome by its Bedrock name (ice_plains) or its Java name (snowy_plains; minecraft: may be left
// out). A Java name several Bedrock biomes show as picks the one with the lowest Bedrock id.
func biomeByName(name string) (world.Biome, bool) {
	name = strings.TrimPrefix(name, "minecraft:")
	if b, ok := world.BiomeByName(name); ok {
		return b, true
	}
	javaNames := v777.Registries["minecraft:worldgen/biome"]
	var found world.Biome
	for _, b := range world.Biomes() {
		id := javamap.Biome(b)
		if int(id) >= len(javaNames) || javaNames[id] != "minecraft:"+name {
			continue
		}
		if found == nil || b.EncodeBiome() < found.EncodeBiome() {
			found = b
		}
	}
	return found, found != nil
}
