package item

import "testing"

func TestMaceSmashBonus(t *testing.T) {
	for _, c := range []struct{ fd, want float64 }{{0, 0}, {1.5, 0}, {2, 8}, {3, 12}, {5, 16}, {8, 22}, {10, 24}, {20, 34}} {
		if got := (Mace{}).SmashBonus(c.fd); got != c.want {
			t.Errorf("SmashBonus(%v) = %v, want %v", c.fd, got, c.want)
		}
	}
}
