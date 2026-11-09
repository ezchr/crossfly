//go:build crossfly_full

package entity

// mobs is set when Crossfly Full's mobs are on ([Mobs] in config.toml): the vanilla mobs of server/mob are
// registered and spawn.
var mobs bool

// Mobs reports whether Crossfly Full's mobs are on.
func Mobs() bool { return mobs }

// SetMobs turns the mobs on or off. It is called once, before the server starts, by whatever registers them.
func SetMobs(on bool) { mobs = on }
