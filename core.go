//go:build !crossfly_full

package main

import (
	"log/slog"

	"github.com/df-mc/dragonfly/server"
)

// fullReleases is where Crossfly Full is released.
const fullReleases = "https://github.com/ezchr/crossfly/releases"

// Edition holds the settings of Crossfly Full's features in config.toml. Crossfly Core has none of them, and
// leaves those sections alone if a config.toml has them.
type Edition struct{}

func defaultEdition() Edition { return Edition{} }

// register registers nothing: Crossfly Core has no blocks, items or entities beyond Dragonfly's.
func (Edition) register(*slog.Logger) error { return nil }

// configure leaves the server configuration as it is.
func (Edition) configure(*server.Config) {}

// start starts nothing.
func (Edition) start(*server.Server, *slog.Logger) {}

// firstStart points to Crossfly Full on the first start of the server.
func firstStart(log *slog.Logger) {
	log.Info("Want mobs, Java redstone and more? Get Crossfly Full: " + fullReleases)
}
