package enchantment_test

import (
	"math"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
)

// The formulas of the enchantments are those of vanilla and keep going past the maximum level,
// as vanilla's do: levels up to 255 (and the int16 of the item NBT beyond that) must not break them.

func approx(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

func TestDamageEnchantmentsAboveMaxLevel(t *testing.T) {
	approx(t, "Sharpness 5", enchantment.Sharpness.Addend(5), 6.25)
	approx(t, "Sharpness 10", enchantment.Sharpness.Addend(10), 12.5)
	approx(t, "Sharpness 255", enchantment.Sharpness.Addend(255), 318.75)
	approx(t, "Power 10", enchantment.Power.PowerDamage(10), 5.5)
	approx(t, "Power 255", enchantment.Power.PowerDamage(255), 128)
	approx(t, "Knockback 10", enchantment.Knockback.Force(10), 5)
	if got := enchantment.FireAspect.Duration(10); got != 40*time.Second {
		t.Errorf("Fire Aspect 10 = %v, want 40s", got)
	}
	if got := enchantment.FireAspect.Duration(255); got != 1020*time.Second {
		t.Errorf("Fire Aspect 255 = %v, want 1020s", got)
	}
}

func TestToolEnchantmentsAboveMaxLevel(t *testing.T) {
	approx(t, "Efficiency 5", enchantment.Efficiency.Addend(5), 26)
	approx(t, "Efficiency 10", enchantment.Efficiency.Addend(10), 101)
	approx(t, "Efficiency 50", enchantment.Efficiency.Addend(50), 2501)
	approx(t, "Efficiency 32767", enchantment.Efficiency.Addend(32767), 32767*32767+1)
	approx(t, "Respiration 10", enchantment.Respiration.Chance(10), 10.0/11)
	approx(t, "Lunge 10 speed", enchantment.Lunge.Speed(10), 4.58)
	approx(t, "Lunge 10 exhaustion", enchantment.Lunge.ExhaustionCost(10), 40)
}

func TestQuickChargeAboveMaxLevel(t *testing.T) {
	for level, want := range map[int]time.Duration{
		1: time.Second, 3: 500 * time.Millisecond, 4: 250 * time.Millisecond,
		5: 0, 6: 0, 10: 0, 255: 0,
	} {
		if got := enchantment.QuickCharge.ChargeDuration(level); got != want {
			t.Errorf("Quick Charge %d = %v, want %v", level, got, want)
		}
	}
}

func TestFireProtectionBurnTicks(t *testing.T) {
	for _, c := range []struct {
		ticks int64
		total int
		want  int64
	}{
		{100, 0, 100},
		{100, 1, 85},
		{160, 4, 64},
		{100, 6, 10},
		{100, 7, 0},  // 1 - 1.05: clamped to 0
		{100, 16, 0}, // four pieces of Fire Protection IV
		{100, 400, 0},
	} {
		if got := enchantment.FireProtection.BurnTicks(c.ticks, c.total); got != c.want {
			t.Errorf("BurnTicks(%d, %d) = %d, want %d", c.ticks, c.total, got, c.want)
		}
	}
}

func TestProtectionFactorCap(t *testing.T) {
	attack := entity.AttackDamageSource{}
	fall := entity.FallDamageSource{}
	prot := func(lvl int) item.Enchantment { return item.NewEnchantment(enchantment.Protection, lvl) }

	approx(t, "Protection 4", enchantment.ProtectionFactor(attack, []item.Enchantment{prot(4)}), 0.16)
	approx(t, "4x Protection 4", enchantment.ProtectionFactor(attack, []item.Enchantment{prot(4), prot(4), prot(4), prot(4)}), 0.64)
	approx(t, "Protection 10", enchantment.ProtectionFactor(attack, []item.Enchantment{prot(10)}), 0.4)
	// The total is capped at 20 points, 80%, as in Bedrock's EnchantUtils::getDamageReduction.
	approx(t, "Protection 20", enchantment.ProtectionFactor(attack, []item.Enchantment{prot(20)}), 0.8)
	approx(t, "Protection 100", enchantment.ProtectionFactor(attack, []item.Enchantment{prot(100)}), 0.8)
	approx(t, "Feather Falling 10", enchantment.ProtectionFactor(fall, []item.Enchantment{item.NewEnchantment(enchantment.FeatherFalling, 10)}), 0.8)
	approx(t, "Feather Falling 5", enchantment.ProtectionFactor(fall, []item.Enchantment{item.NewEnchantment(enchantment.FeatherFalling, 5)}), 0.6)
	approx(t, "Fire Protection 10 vs attack", enchantment.ProtectionFactor(attack, []item.Enchantment{item.NewEnchantment(enchantment.FireProtection, 10)}), 0)
}

func TestUnbreakingAboveMaxLevel(t *testing.T) {
	// A tool takes damage with a chance of 1/(level+1), armour with 0.6+0.4/(level+1).
	const n = 200000
	sword := item.Sword{Tier: item.ToolTierDiamond}
	chest := item.Chestplate{Tier: item.ArmourTierDiamond{}}
	for _, c := range []struct {
		name  string
		it    world.Item
		level int
		want  float64
	}{
		{"sword 3", sword, 3, 0.25},
		{"sword 10", sword, 10, 1.0 / 11},
		{"sword 255", sword, 255, 1.0 / 256},
		{"chestplate 10", chest, 10, 0.6 + 0.4/11},
	} {
		got := float64(enchantment.Unbreaking.Reduce(c.it, c.level, n)) / n
		if math.Abs(got-c.want) > 0.01 {
			t.Errorf("Unbreaking %s: %.4f of the damage taken, want %.4f", c.name, got, c.want)
		}
	}
}
