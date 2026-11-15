package javasession

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world/sound"
)

func TestAttackStrength(t *testing.T) {
	sword := 4 + javaAttackSpeedModifier(item.Sword{Tier: item.ToolTierDiamond}) // 1.6: 12.5 ticks
	for _, c := range []struct {
		ticks int
		speed float64
		full  bool
	}{
		{0, sword, false},
		{10, sword, false},
		{11, sword, true}, // (11+0.5)/12.5 = 0.92
		{40, sword, true},
		{4, 4, false}, // empty hand: 5 ticks
		{5, 4, true},
		{100, 0, false},
	} {
		if got := attackStrength(c.ticks, c.speed) > 0.9; got != c.full {
			t.Errorf("attackStrength(%d, %v) full = %v, want %v", c.ticks, c.speed, got, c.full)
		}
	}
	if m := javaAttackSpeedModifier(item.Axe{Tier: item.ToolTierIron}); m != -3.1 {
		t.Errorf("iron axe modifier %v", m)
	}
	if m := javaAttackSpeedModifier(item.Hoe{Tier: item.ToolTierDiamond}); m != 0 {
		t.Errorf("diamond hoe modifier %v", m)
	}
}

// TestAttackSounds checks that an attack plays what vanilla Java's Player.attack plays.
func TestAttackSounds(t *testing.T) {
	for _, c := range []struct {
		a    sound.Attack
		want []jsound
	}{
		{sound.Attack{Damage: true}, []jsound{sndAttackStrong}},
		{sound.Attack{Damage: true, Weak: true}, []jsound{sndAttackWeak}},
		{sound.Attack{}, []jsound{sndAttackNoDamage}},
		{sound.Attack{Air: true}, nil},
		{sound.Attack{Damage: true, Critical: true}, []jsound{sndAttackCrit}},
		{sound.Attack{Damage: true, Sweep: true}, []jsound{sndAttackSweep}},
		{sound.Attack{Damage: true, Knockback: true}, []jsound{sndAttackKnockback, sndAttackStrong}},
		{sound.Attack{Knockback: true}, []jsound{sndAttackKnockback, sndAttackNoDamage}},
	} {
		var got []jsound
		attackSoundsOf(c.a, func(s jsound) { got = append(got, s) })
		if len(got) != len(c.want) {
			t.Errorf("%+v: got %v, want %v", c.a, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%+v: got %v, want %v", c.a, got, c.want)
			}
		}
	}
}
