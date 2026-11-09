//go:build crossfly_full

package world

// fullBuild is true in Crossfly Full builds.
const fullBuild = true

// javaRedstone is set when worlds follow Java Edition's redstone rules: its tick order, scheduled tick
// priorities, shape updates, chained neighbour updates and conductors (Crossfly Full, [Redstone] in
// config.toml). When off, worlds behave as in Crossfly Core.
var javaRedstone = true

// JavaRedstone reports whether worlds follow Java Edition's redstone rules.
func JavaRedstone() bool { return javaRedstone }

// SetJavaRedstone turns Java Edition's redstone rules on or off. It must be called before any world is
// created and before blocks are registered (block.RegisterJavaRedstone).
func SetJavaRedstone(on bool) { javaRedstone = on }
