//go:build crossfly_full

package core

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

// fakeEntity stands in for the exploding entity; sourceEntity only passes
// it on.
type fakeEntity struct{ world.Entity }

// TestSourceEntityExplosion: an entity explosion names the entity that
// exploded (the charged creeper of a head drop); a block explosion names
// nobody.
func TestSourceEntityExplosion(t *testing.T) {
	c := &fakeEntity{}
	src := entity.ExplosionDamageSource{Source: world.EntityExplosionSource{Entity: c, ExplosionSize: 3}}
	if e := sourceEntity(src); e != world.Entity(c) {
		t.Errorf("entity explosion source: %v", e)
	}
	if e := sourceEntity(entity.ExplosionDamageSource{Source: world.BlockExplosionSource{Block: block.TNT{}}}); e != nil {
		t.Errorf("block explosion source: %v", e)
	}
}
