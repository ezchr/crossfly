//go:build crossfly_full

package entity

import "github.com/df-mc/dragonfly/server/world"

// mobs is set when Crossfly Full's mobs are on ([Mobs] in config.toml): the vanilla mobs of server/mob are
// registered and spawn.
var mobs bool

// Mobs reports whether Crossfly Full's mobs are on.
func Mobs() bool { return mobs }

// SetMobs turns the mobs on or off. It is called once, before the server starts, by whatever registers them.
func SetMobs(on bool) { mobs = on }

// javaMobs is set when Crossfly Full runs its mobs as in Java Edition (mobs=java, package server/mob/javamob): item
// entities then move as in Java Edition too (item_java.go), whatever the redstone.
var javaMobs bool

// JavaMobs reports whether Java mob mode is on.
func JavaMobs() bool { return javaMobs }

// SetJavaMobs turns Java mob mode on or off. It is called once, before the server starts, by javamob.Install.
func SetJavaMobs(on bool) { javaMobs = on }

// ApplyFluidCurrents pushes e along the water and lava it is in as Java's EntityFluidInteraction does and returns how
// high water and lava reach over its bottom (0 when it is not in them). Java mobs (server/mob/javamob) call it from
// their fluid update.
func ApplyFluidCurrents(e *Ent, tx *world.Tx) (water, lava float64) { return applyFluidCurrents(e, tx) }
