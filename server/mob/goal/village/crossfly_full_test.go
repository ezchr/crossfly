//go:build crossfly_full

package village_test

import (
	"github.com/df-mc/dragonfly/server/entity"
	village "github.com/df-mc/dragonfly/server/mob/component/village"
)

// The tests run with the mobs on, as vanilla.Register sets them up.
func init() {
	entity.SetMobs(true)
	village.Enable()
}
