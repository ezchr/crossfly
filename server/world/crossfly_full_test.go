//go:build crossfly_full

package world_test

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
)

// The tests of Crossfly Full run with Java redstone, its blocks registered.
func init() {
	block.RegisterJavaRedstone()
	item.RegisterJavaRedstone()
}
