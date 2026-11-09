package item

import "testing"

func TestSpearIsChargeable(t *testing.T) {
	if _, ok := any(Spear{Tier: ToolTierIron}).(Chargeable); !ok {
		t.Fatal("Spear is not Chargeable")
	}
}

// TestSpearBedrockValues checks spear values against Bedrock's vanilla item data.
func TestSpearBedrockValues(t *testing.T) {
	durability := map[ToolTier]int{
		ToolTierWood: 60, ToolTierGold: 30, ToolTierStone: 130, ToolTierCopper: 190,
		ToolTierIron: 250, ToolTierDiamond: 1560, ToolTierNetherite: 2030,
	}
	for tier, want := range durability {
		if got := (Spear{Tier: tier}).DurabilityInfo().MaxDurability; got != want {
			t.Errorf("%v spear max durability %v, want %v", tier.Name, got, want)
		}
	}
	if m := (Spear{Tier: ToolTierIron}).HitboxMargin(); m != 0.125 {
		t.Errorf("hitboxes grown by %v each side, want half of hitbox_margin 0.25", m)
	}
}

// TestSpearChargeDismount: an iron spear dismounts from the end of its 12 tick delay until
// dismount_conditions' max_duration of 50 ticks later, at 11 blocks per second or more.
func TestSpearChargeDismount(t *testing.T) {
	s := Spear{Tier: ToolTierIron}
	for _, c := range []struct {
		ticks int64
		want  bool
	}{{11, false}, {12, true}, {61, true}, {62, false}} {
		if got := s.ChargeDismount(ticks(c.ticks)); got != c.want {
			t.Errorf("dismount after %v ticks: %v, want %v", c.ticks, got, c.want)
		}
	}
	if got := s.ChargeDismountSpeedRequirement(); got != 11 {
		t.Errorf("iron spear dismount speed %v, want 11", got)
	}
}
