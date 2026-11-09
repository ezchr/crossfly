//go:build !crossfly_full

package javasession

import (
	jserver "github.com/df-mc/dragonfly/server/java/protocol/server"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/world"
)

// Crossfly Core has no Java redstone: no block is a JavaBlock and there are no minecarts, so these are never called
// (piston.go and minecart.go in Crossfly Full).

func javaBlockEntityData(JavaBlock) (string, []byte) { return "", nil }
func (s *Session) viewMinecart(world.Entity, int32)  {}

// Crossfly Core shows no capes of Java players to Bedrock players (cape.go in Crossfly Full).

func capesOn() bool                                         { return false }
func capeTextureURL([]jserver.Property) string              { return "" }
func withCape(s skin.Skin, _ skin.Cape, _ string) skin.Skin { return s }
func fetchCape(string) func() (skin.Cape, string, bool) {
	return func() (skin.Cape, string, bool) { return skin.Cape{}, "", false }
}
