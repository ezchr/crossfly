//go:build crossfly_full

package loot_test

import (
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob/component/loot"
)

// The tests run with the mobs on, as vanilla.Register sets them up.
func init() {
	entity.SetMobs(true)
	loot.Enable()
}
