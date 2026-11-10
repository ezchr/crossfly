//go:build !crossfly_full

package entity

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Crossfly Core has no Java redstone: world.JavaRedstone is always false, so these are never called and the compiler
// leaves them out. Crossfly Full has the real ones (*_java.go).

func (m *Movement) checkSteppers(*world.Tx)                     {}
func checkRedstoneInsiders(*world.Tx, world.Entity, mgl64.Vec3) {}
func (b *tntBehaviour) javaTick(*Ent, *world.Tx) *Movement      { return nil }
func (i *ItemBehaviour) javaTick(*Ent, *world.Tx) *Movement     { return nil }

type javaItem struct{}

// Mobs reports whether Crossfly Full's mobs are on: never in Crossfly Core.
func Mobs() bool { return false }
