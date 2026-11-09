//go:build !crossfly_full

package javasession

import "github.com/df-mc/dragonfly/server/world"

// Crossfly Core has no Java redstone: no block is a JavaBlock and there are no minecarts, so these are never called
// (piston.go and minecart.go in Crossfly Full).

func javaBlockEntityData(JavaBlock) (string, []byte) { return "", nil }
func (s *Session) viewMinecart(world.Entity, int32)  {}

