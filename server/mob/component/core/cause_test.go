//go:build crossfly_full

package core_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/world"
)

// TestCauseOfExplosions: a creeper blast (an entity explosion source) is
// entity_explosion, a bed or TNT block blast block_explosion.
func TestCauseOfExplosions(t *testing.T) {
	if got := core.CauseOf(entity.ExplosionDamageSource{Source: world.EntityExplosionSource{}}); got != "entity_explosion" {
		t.Errorf("entity explosion: %q", got)
	}
	if got := core.CauseOf(entity.ExplosionDamageSource{Source: world.BlockExplosionSource{}}); got != "block_explosion" {
		t.Errorf("block explosion: %q", got)
	}
}
