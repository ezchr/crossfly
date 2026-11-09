package main

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/generator"
)

// flatConfig sets up the flat overworld new chunks are generated with.
type flatConfig struct {
	// Layers are the blocks of the flat overworld from the top down, by item name ("minecraft:" may be left out).
	Layers []string `comment:"Layers are the blocks of the flat overworld from the top down, by item name (minecraft: may be left out), e.g. [\"snow_layer\", \"grass_block\", \"dirt\", \"dirt\", \"bedrock\"]. Only new chunks use them."`
	// Biome is the biome of the flat overworld, e.g. plains or snowy_plains.
	Biome string `comment:"Biome is the biome of the flat overworld, e.g. plains or snowy_plains."`
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
	bi, ok := world.BiomeByName(f.Biome)
	if !ok {
		return fmt.Errorf("flat world: unknown biome %q", f.Biome)
	}
	overworld := generator.NewFlat(bi, layers)
	conf.Generator = func(dim world.Dimension) world.Generator {
		switch dim {
		case world.Nether:
			return generator.NewFlat(biome.NetherWastes{}, []world.Block{block.Netherrack{}, block.Netherrack{}, block.Netherrack{}, block.Bedrock{}})
		case world.End:
			return generator.NewFlat(biome.End{}, []world.Block{block.EndStone{}, block.EndStone{}, block.EndStone{}, block.Bedrock{}})
		}
		return overworld
	}
	return nil
}
