//go:build crossfly_full

package jnether

import (
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/hostile"
	"github.com/go-gl/mathgl/mgl64"
)

// MagmaCubeID is the magma cube's identifier.
const MagmaCubeID = "minecraft:magma_cube"

// magmaKind is MagmaCube on AbstractCubeMob: jump delay x4, squish 0.9,
// always deals contact damage (canDealDamage = isEffectiveAi), +2 attack
// damage, damage and xp = size, armour = size * 3.
var magmaKind = &hostile.CubeKind{
	ID: MagmaCubeID, Width: 0.52, Height: 0.52, EyeHeight: 0.325,
	JumpDelayFactor: 4, SquishDecay: 0.9, AlwaysDealsDamage: true, AttackDamageBonus: 2,
	SizeIsDamageAndXP: true,
	OnSetSize: func(m *javamob.Mob, size int) {
		m.Attributes().Instance(javamob.AttrArmor).SetBase(float64(size * 3))
	},
	JumpSound: "entity.magma_cube.jump", JumpSoundSmall: "entity.magma_cube.jump",
	SquishSound: "entity.magma_cube.squish", SquishSoundSmall: "entity.magma_cube.squish_small",
}

func init() {
	javamob.Register(javamob.Type{
		ID: MagmaCubeID, Category: javamob.CategoryMonster,
		Width: magmaKind.Width, Height: magmaKind.Height, EyeHeight: magmaKind.EyeHeight,
		FireImmune: true, NotInPeaceful: true,
		Families: []string{"magmacube", "monster", "mob"},
		// MagmaCube.createAttributes.
		Attributes: func() map[javamob.Attribute]float64 {
			a := javamob.MonsterAttributes()
			a[javamob.AttrMovementSpeed] = float64(float32(0.2))
			return a
		},
		New: func() javamob.Behaviour { return &MagmaCube{CubeMob: hostile.NewCubeMob(magmaKind)} },
	})
}

// MagmaCube is the MagmaCube class: AbstractCubeMob with Slime's goals
// (the same targeting: players within 4 blocks of height, iron golems) and
// its own jumps.
type MagmaCube struct {
	*hostile.CubeMob
}

// JumpFromGround is MagmaCube.jumpFromGround: the jump power plus 0.1 per
// size, whatever the vertical speed was.
func (c *MagmaCube) JumpFromGround(m *javamob.Mob) bool {
	v := m.Velocity()
	m.SetVelocity(mgl64.Vec3{v[0], m.JumpPower() + float64(float32(c.Size())*0.1), v[2]})
	m.SyncMovement()
	return true
}

// JumpInLiquid is MagmaCube.jumpInLiquid: in lava 0.22 + 0.05 per size up;
// water is the LivingEntity jump.
func (c *MagmaCube) JumpInLiquid(m *javamob.Mob, lava bool) bool {
	if !lava {
		return false
	}
	v := m.Velocity()
	m.SetVelocity(mgl64.Vec3{v[0], float64(float32(0.22) + float32(c.Size())*0.05), v[2]})
	m.SyncMovement()
	return true
}

// FilterDamage: the type is fire immune (the base does not filter fire
// damage for fireImmune types yet).
func (c *MagmaCube) FilterDamage(_ *javamob.Mob, d *mob.Damage) { fireImmune(d) }
