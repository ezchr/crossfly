//go:build !crossfly_full

package main

import (
	"log/slog"

	"github.com/df-mc/dragonfly/server"
)

// fullReleases is where Crossfly Full is released.
const fullReleases = "https://github.com/ezchr/crossfly/releases"

// Edition holds the settings of Crossfly Full's features in config.toml. Crossfly Core has none of them: no
// commands, operators or console, no flat world option, no mobs, Java redstone or capes. A config.toml with
// those sections still loads, and they are ignored.
type Edition struct{}

func defaultEdition() Edition { return Edition{} }

// register registers nothing: Crossfly Core has no blocks, items or entities beyond Dragonfly's.
func (Edition) register(*slog.Logger) error { return nil }

// configure leaves the server configuration as it is.
func (Edition) configure(*server.Config) error { return nil }

// start says how to stop the server: Crossfly Core reads no commands from the console, and the server stops on
// Ctrl+C (SIGINT) or SIGTERM (server.Server.CloseOnProgramEnd).
func (Edition) start(_ *server.Server, log *slog.Logger) {
	log.Info("Stop the server with Ctrl+C (SIGINT or SIGTERM). Crossfly Core has no commands or console.")
}

// firstStart points to Crossfly Full on the first start of the server.
func firstStart(log *slog.Logger) {
	log.Info("Want commands, mobs, Java redstone and more? Get Crossfly Full: " + fullReleases)
}
