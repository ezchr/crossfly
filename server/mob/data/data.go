//go:build crossfly_full

// Package data embeds the vanilla Bedrock 1.26.63 behaviour pack entity
// definitions and spawn rules, merged in pack-stack order (vanilla, then
// vanilla_1.14 ... vanilla_1.26.63, the newest file per identifier wins).
// The files are the plain minified JSON from the packs' __brarchive blobs.
//
// To refresh: extract the BDS packs' entities.brarchive/spawn_rules.brarchive,
// merge them by identifier in version order and replace entities/ and
// spawn_rules/ (one <name>.json per identifier). npc.json.json, a stale
// override without an identifier, is left out.
package data

import (
	"embed"
	"io/fs"
	"path"
	"slices"
)

//go:embed entities/*.json
var entities embed.FS

//go:embed spawn_rules/*.json
var spawnRules embed.FS

// File is one embedded JSON file.
type File struct {
	// Name is the file name, e.g. "zombie.json".
	Name string
	Data []byte
}

// Entities returns the 129 entity definition files, sorted by name.
func Entities() []File { return files(entities, "entities") }

// SpawnRules returns the spawn rule files, sorted by name.
func SpawnRules() []File { return files(spawnRules, "spawn_rules") }

// Entity returns one entity file by name ("zombie.json").
func Entity(name string) ([]byte, bool) {
	b, err := entities.ReadFile(path.Join("entities", name))
	return b, err == nil
}

// SpawnRule returns one spawn rule file by name.
func SpawnRule(name string) ([]byte, bool) {
	b, err := spawnRules.ReadFile(path.Join("spawn_rules", name))
	return b, err == nil
}

func files(fsys embed.FS, dir string) []File {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		panic(err)
	}
	out := make([]File, 0, len(entries))
	for _, e := range entries {
		b, err := fsys.ReadFile(path.Join(dir, e.Name()))
		if err != nil {
			panic(err)
		}
		out = append(out, File{Name: e.Name(), Data: b})
	}
	slices.SortFunc(out, func(a, b File) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	return out
}
