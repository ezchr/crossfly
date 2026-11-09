//go:build crossfly_full

package block

import "github.com/df-mc/dragonfly/server/item"

// The tests of Crossfly Full run with Java redstone, its blocks registered.
func init() {
	RegisterJavaRedstone()
	item.RegisterJavaRedstone()
}
