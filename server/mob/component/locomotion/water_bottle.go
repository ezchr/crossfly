//go:build crossfly_full

package locomotion

import (
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/filter"
)

// waterBottleFlag answers in_contact_with_water for a mob a splash water
// bottle just hit (Actor::isInContactWithWater: water, rain or
// isAffectedByWaterBottle). The water and rain cases fall through to the
// filter default.
func waterBottleFlag(m *mob.Mob, f filter.Flag) (bool, bool) {
	if f == filter.FlagInContactWithWater && entity.AffectedByWaterBottle(m) {
		return true, true
	}
	return false, false
}
