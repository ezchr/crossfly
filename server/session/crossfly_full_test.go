//go:build crossfly_full

package session

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
)

// The tests of Crossfly Full run with Java redstone and the mob items registered.
func init() {
	block.RegisterJavaRedstone()
	item.RegisterJavaRedstone()
	item.RegisterMobItems()
}
