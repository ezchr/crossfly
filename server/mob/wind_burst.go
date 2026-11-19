//go:build crossfly_full

package mob

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// A wind burst (block.WindBurst) pushes a mob along the line from its centre to the mob's eyes, less the
// mob's knockback_resistance (KnockbackRules::getScaledKnockbackForce).
func init() {
	block.WindBurstEntityInfo = func(e world.Entity) (mgl64.Vec3, float64, bool) {
		m, ok := Of(e)
		if !ok {
			return mgl64.Vec3{}, 0, false
		}
		return m.EyePosition(), m.Attr(AttrKnockbackResistance), true
	}
}
