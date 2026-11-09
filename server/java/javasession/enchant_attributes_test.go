package javasession

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
)

func TestEnchantAttributeValues(t *testing.T) {
	ench := func(it world.Item, et item.EnchantmentType, lvl int) item.Stack {
		return item.NewStack(it, 1).WithEnchantments(item.NewEnchantment(et, lvl))
	}
	none := [4]item.Stack{}
	if v := enchantAttributeValues(item.Stack{}, none, 0); v != enchantAttrDefaults {
		t.Fatalf("no enchantments: %v, want the defaults %v", v, enchantAttrDefaults)
	}
	pick := item.Pickaxe{Tier: item.ToolTierDiamond}
	for level, want := range map[int]float64{1: 2, 5: 26, 10: 101, 32: 1024, 255: 1024} {
		if v := enchantAttributeValues(ench(pick, enchantment.Efficiency, level), none, 0); v[attrMiningEfficiency] != want {
			t.Errorf("Efficiency %d: mining_efficiency %v, want %v", level, v[attrMiningEfficiency], want)
		}
	}
	helmet := item.Helmet{Tier: item.ArmourTierDiamond{}}
	if v := enchantAttributeValues(item.Stack{}, [4]item.Stack{ench(helmet, enchantment.AquaAffinity, 5)}, 0); v[attrSubmergedMiningSpeed] != 1 {
		t.Errorf("Aqua Affinity V: submerged_mining_speed %v, want 1", v[attrSubmergedMiningSpeed])
	}
	if v := enchantAttributeValues(item.Stack{}, [4]item.Stack{ench(helmet, enchantment.Respiration, 10)}, 0); v[attrOxygenBonus] != 10 {
		t.Errorf("Respiration X: oxygen_bonus %v, want 10", v[attrOxygenBonus])
	}
	legs := item.Leggings{Tier: item.ArmourTierDiamond{}}
	for level, want := range map[int]float64{1: 0.45, 3: 0.75, 4: 0.9, 5: 1, 100: 1} {
		v := enchantAttributeValues(item.Stack{}, [4]item.Stack{2: ench(legs, enchantment.SwiftSneak, level)}, 0)
		if math.Abs(v[attrSneakingSpeed]-want) > 1e-9 {
			t.Errorf("Swift Sneak %d: sneaking_speed %v, want %v", level, v[attrSneakingSpeed], want)
		}
	}
	boots := item.Boots{Tier: item.ArmourTierDiamond{}}
	for level, want := range map[int]float64{1: 1.0 / 3, 3: 1, 10: 1} {
		v := enchantAttributeValues(item.Stack{}, [4]item.Stack{3: ench(boots, enchantment.DepthStrider, level)}, 0)
		if math.Abs(v[attrWaterMovementEfficiency]-want) > 1e-9 {
			t.Errorf("Depth Strider %d: water_movement_efficiency %v, want %v", level, v[attrWaterMovementEfficiency], want)
		}
	}
	if v := enchantAttributeValues(item.Stack{}, none, 3); v[attrMovementEfficiency] != 1 {
		t.Errorf("Soul Speed in effect: movement_efficiency %v, want 1", v[attrMovementEfficiency])
	}
}

func TestSoulSpeed(t *testing.T) {
	boots := item.NewStack(item.Boots{Tier: item.ArmourTierDiamond{}}, 1).WithEnchantments(item.NewEnchantment(enchantment.SoulSpeed, 10))
	for _, c := range []struct {
		name                                     string
		onSoul, onGround, active, flying, riding bool
		want                                     int
	}{
		{"on soul sand", true, true, false, false, false, 10},
		{"jumping off soul sand", false, false, true, false, false, 10},
		{"landed elsewhere", false, true, true, false, false, 0},
		{"in the air, not from soul sand", false, false, false, false, false, 0},
		{"flying", true, true, true, true, false, 0},
		{"riding", true, true, true, false, true, 0},
	} {
		if got := soulSpeedLevel(boots, c.onSoul, c.onGround, c.active, c.flying, c.riding); got != c.want {
			t.Errorf("%s: level %d, want %d", c.name, got, c.want)
		}
	}
	if got := soulSpeedLevel(item.NewStack(item.Boots{Tier: item.ArmourTierDiamond{}}, 1), true, true, false, false, false); got != 0 {
		t.Errorf("boots without Soul Speed: level %d", got)
	}
	// 0.03+0.0105*level on top of the 0.1 base speed: Soul Speed III walks at 0.1615 (Java's
	// 0.0405+0.0105*2 = 0.0615 extra).
	if got := 0.1 * soulSpeedFactor(3); math.Abs(got-0.1615) > 1e-9 {
		t.Errorf("Soul Speed III speed %v, want 0.1615", got)
	}
	if soulSpeedFactor(0) != 1 {
		t.Errorf("no Soul Speed changes the speed")
	}
}
