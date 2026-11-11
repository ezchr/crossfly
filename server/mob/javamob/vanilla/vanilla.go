//go:build crossfly_full

// Package vanilla links every Java mob package, so that importing it registers all Java mob types. Each mob
// package adds one blank import line here; the server calls javamob.Install after importing this package
// (full.go, mobs=java).
package vanilla

import (
	"github.com/df-mc/dragonfly/server/mob/javamob"

	// The Java mob packages, one line each.
	_ "github.com/df-mc/dragonfly/server/mob/javamob/hostile"
	_ "github.com/df-mc/dragonfly/server/mob/javamob/monster"
	_ "github.com/df-mc/dragonfly/server/mob/jfarm"
	_ "github.com/df-mc/dragonfly/server/mob/jnether"
	_ "github.com/df-mc/dragonfly/server/mob/jpets"
	_ "github.com/df-mc/dragonfly/server/mob/jraider"
	_ "github.com/df-mc/dragonfly/server/mob/jvillage"
)

// Install registers the Java mob types of every linked package in package mob (javamob.Install).
func Install() { javamob.Install() }
