//go:build crossfly_full

package javaspawn

import (
	"slices"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/java/javamap"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/world"
)

// biomeInfo is the Java biome a Dragonfly biome maps to (the biome Java
// clients see), with its spawn settings and tags.
type biomeInfo struct {
	name     string
	settings *MobSpawnSettings
	tags     map[string]bool
}

var (
	biomeInfos sync.Map // Bedrock biome id -> *biomeInfo
	noSpawns   = &MobSpawnSettings{}
)

// BiomeName returns the Java biome name (without namespace) natural spawning
// uses for a Dragonfly biome: the one Crossfly sends Java clients.
func BiomeName(b world.Biome) string { return biomeOf(b).name }

func biomeOf(b world.Biome) *biomeInfo {
	id := b.EncodeBiome()
	if v, ok := biomeInfos.Load(id); ok {
		return v.(*biomeInfo)
	}
	names := v777.Registries["minecraft:worldgen/biome"]
	name := "plains"
	if j := javamap.BiomeID(id); j >= 0 && int(j) < len(names) {
		name = strings.TrimPrefix(names[j], "minecraft:")
	}
	in := &biomeInfo{name: name, settings: biomeSpawns[name], tags: map[string]bool{}}
	if in.settings == nil {
		in.settings = noSpawns
	}
	for t, names := range biomeTags {
		if slices.Contains(names, name) {
			in.tags[t] = true
		}
	}
	v, _ := biomeInfos.LoadOrStore(id, in)
	return v.(*biomeInfo)
}

// biomeAt returns the biome info at pos.
func (e *env) biomeAt(pos cube.Pos) *biomeInfo { return biomeOf(e.tx.Biome(pos)) }

// mobsAt is NaturalSpawner.mobsAt: the fortress enemies on nether bricks
// inside a fortress, the spawn override of a structure at pos
// (ChunkGenerator.getMobsAt), else the biome's list.
func (e *env) mobsAt(cat Category, pos cube.Pos) []Spawner {
	if e.cfg.Structures != nil {
		structures := e.cfg.Structures(e.tx, pos)
		if cat == CategoryMonster && len(structures) > 0 && javaName(e.tx.Block(pos.Side(cube.FaceDown))) == "nether_bricks" {
			for _, s := range structures {
				if s.Name == "fortress" {
					return structureOverrides["fortress"][CategoryMonster].Spawns
				}
			}
		}
		for _, s := range structures {
			if o, ok := structureOverrides[s.Name][cat]; ok && (!o.Piece || s.InPiece) {
				return o.Spawns
			}
		}
	}
	return e.biomeAt(pos).settings.Spawns[cat]
}
