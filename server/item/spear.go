package item

import (
	"time"

	"github.com/df-mc/dragonfly/server/world"
)

// The spear values below are those of Bedrock's vanilla item data (the minecraft:piercing_weapon and
// minecraft:kinetic_weapon components).
const (
	// spearHitboxMargin is the hitbox_margin of the spear components. Bedrock grows target hitboxes by half
	// of it on each side, so a hitbox ends up hitbox_margin wider and taller in total.
	spearHitboxMargin               = 0.25
	spearMinAttackRange             = 2
	spearMaxAttackRange             = 4.5
	spearCreativeMaxAttackRange     = 7.5
	spearChargeDamageRequirement    = 4.6
	spearChargeKnockbackRequirement = 5.1
)

// Spear is a tiered melee weapon with longer reach and a charge attack.
type Spear struct {
	// Tier is the tier of the spear.
	Tier ToolTier
}

// AttackDamage returns the jab attack damage of the spear.
func (s Spear) AttackDamage() float64 {
	return s.Tier.BaseAttackDamage
}

// MaxCount always returns 1.
func (s Spear) MaxCount() int {
	return 1
}

// ToolType returns the tool type for spears.
func (s Spear) ToolType() ToolType {
	return TypeSpear
}

// HarvestLevel returns the harvest level of the spear tier.
func (s Spear) HarvestLevel() int {
	return s.Tier.HarvestLevel
}

// BaseMiningEfficiency always returns 1.
func (s Spear) BaseMiningEfficiency(world.Block) float64 {
	return 1
}

// DurabilityInfo ...
func (s Spear) DurabilityInfo() DurabilityInfo {
	return DurabilityInfo{
		MaxDurability:    s.maxDurability(),
		BrokenItem:       simpleItem(Stack{}),
		AttackDurability: 1,
	}
}

// SmeltInfo ...
func (s Spear) SmeltInfo() SmeltInfo {
	switch s.Tier {
	case ToolTierIron:
		return newOreSmeltInfo(NewStack(IronNugget{}, 1), 0.1)
	case ToolTierGold:
		return newOreSmeltInfo(NewStack(GoldNugget{}, 1), 0.1)
	case ToolTierCopper:
		return newOreSmeltInfo(NewStack(CopperNugget{}, 1), 0.1)
	}
	return SmeltInfo{}
}

// FuelInfo ...
func (s Spear) FuelInfo() FuelInfo {
	if s.Tier == ToolTierWood {
		return newFuelInfo(time.Second * 10)
	}
	return FuelInfo{}
}

// RepairableBy ...
func (s Spear) RepairableBy(i Stack) bool {
	return toolTierRepairable(s.Tier)(i)
}

// EnchantmentValue ...
func (s Spear) EnchantmentValue() int {
	return s.Tier.EnchantmentValue
}

// Cooldown returns the forced jab cooldown of the spear.
func (s Spear) Cooldown() time.Duration {
	switch s.Tier {
	case ToolTierWood:
		return ticks(13)
	case ToolTierStone:
		return ticks(15)
	case ToolTierCopper:
		return ticks(17)
	case ToolTierGold, ToolTierIron:
		return ticks(19)
	case ToolTierDiamond:
		return ticks(21)
	case ToolTierNetherite:
		return ticks(23)
	}
	return 0
}

// CooldownCategory returns the shared cooldown category for all spear tiers.
func (s Spear) CooldownCategory() string {
	return "spear"
}

// AttackRange returns the minimum and maximum jab range of the spear.
func (s Spear) AttackRange(creative bool) (minRange, maxRange float64) {
	if creative {
		return spearMinAttackRange, spearCreativeMaxAttackRange
	}
	return spearMinAttackRange, spearMaxAttackRange
}

// HitboxMargin returns how far target hitboxes are grown on each side for spear attacks: half of the
// spear's hitbox_margin.
func (s Spear) HitboxMargin() float64 {
	return spearHitboxMargin / 2
}

// ChargeMultiplier returns the charge attack damage multiplier of the spear.
func (s Spear) ChargeMultiplier() float64 {
	switch s.Tier {
	case ToolTierWood, ToolTierGold:
		return 0.7
	case ToolTierStone, ToolTierCopper:
		return 0.82
	case ToolTierIron:
		return 0.95
	case ToolTierDiamond:
		return 1.075
	case ToolTierNetherite:
		return 1.2
	}
	return 0
}

// ChargeDamageSpeedRequirement returns the relative blocks-per-second speed needed for charge damage.
func (s Spear) ChargeDamageSpeedRequirement() float64 {
	return spearChargeDamageRequirement
}

// ChargeKnockbackSpeedRequirement returns the attacker blocks-per-second speed needed for charge knockback.
func (s Spear) ChargeKnockbackSpeedRequirement() float64 {
	return spearChargeKnockbackRequirement
}

// ChargeDismountSpeedRequirement returns the attacker blocks-per-second speed a charge attack needs to
// dismount its target.
func (s Spear) ChargeDismountSpeedRequirement() float64 {
	switch s.Tier {
	case ToolTierWood:
		return 14
	case ToolTierStone, ToolTierGold:
		return 13
	case ToolTierCopper:
		return 12
	case ToolTierIron:
		return 11
	case ToolTierDiamond:
		return 10
	case ToolTierNetherite:
		return 9
	}
	return 0
}

// ChargeDismount reports whether a charge attack may still dismount its target for the charge duration:
// only in the first stage of the charge.
func (s Spear) ChargeDismount(duration time.Duration) bool {
	delay, stage1, _, _ := s.chargeTimings()
	return duration >= delay && duration-delay < stage1
}

// ChargeAttack returns whether a charge attack may deal damage and knockback for the charge duration.
func (s Spear) ChargeAttack(duration time.Duration) (damage, knockback bool) {
	delay, stage1, stage2, stage3 := s.chargeTimings()
	if duration < delay {
		return false, false
	}
	elapsed := duration - delay
	switch {
	case elapsed < stage1:
		return true, true
	case elapsed < stage1+stage2:
		return true, true
	case elapsed < stage1+stage2+stage3:
		return true, false
	default:
		return false, false
	}
}

// Charge ...
func (s Spear) Charge(Releaser, *world.Tx, *UseContext, time.Duration) bool {
	return false
}

// ContinueCharge ...
func (s Spear) ContinueCharge(Releaser, *world.Tx, *UseContext, time.Duration) {}

// ReleaseCharge ...
func (s Spear) ReleaseCharge(Releaser, *world.Tx, *UseContext) bool {
	return false
}

// CanCharge always returns true: holding use with a spear always starts a
// charge. Without it the spear is not a Chargeable (the interface gained
// CanCharge with the crossbow fix), so using it never set the player as
// using an item and the charge attack could never happen.
func (s Spear) CanCharge(Releaser, *world.Tx, *UseContext) bool {
	return true
}

// EncodeItem ...
func (s Spear) EncodeItem() (name string, meta int16) {
	return "minecraft:" + s.Tier.Name + "_spear", 0
}

// maxDurability returns the max_durability of the spear tier, which for spears is not the tier's tool
// durability.
func (s Spear) maxDurability() int {
	switch s.Tier {
	case ToolTierWood:
		return 60
	case ToolTierGold:
		return 30
	case ToolTierStone:
		return 130
	case ToolTierCopper:
		return 190
	case ToolTierIron:
		return 250
	case ToolTierDiamond:
		return 1560
	case ToolTierNetherite:
		return 2030
	}
	return s.Tier.Durability
}

// chargeTimings returns the delay before a charge attack starts and the length of its three stages: the
// first ends at dismount_conditions' max_duration, the second at knockback_conditions' and the third at
// damage_conditions'.
func (s Spear) chargeTimings() (delay, stage1, stage2, stage3 time.Duration) {
	switch s.Tier {
	case ToolTierWood:
		return ticks(15), time.Second * 5, time.Second * 5, time.Second * 5
	case ToolTierGold:
		return ticks(14), ticks(70), time.Second * 5, ticks(105)
	case ToolTierStone:
		return ticks(14), ticks(90), ticks(90), ticks(95)
	case ToolTierCopper:
		return ticks(13), time.Second * 4, ticks(85), ticks(85)
	case ToolTierIron:
		return ticks(12), ticks(50), ticks(85), ticks(90)
	case ToolTierDiamond:
		return ticks(10), time.Second * 3, ticks(70), ticks(70)
	case ToolTierNetherite:
		return ticks(8), ticks(50), time.Second * 3, ticks(65)
	}
	return 0, 0, 0, 0
}

func ticks(t int64) time.Duration {
	return time.Duration(t) * time.Second / 20
}
