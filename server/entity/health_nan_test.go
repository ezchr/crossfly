package entity

import (
	"math"
	"testing"
)

func TestHealthManagerNonFinite(t *testing.T) {
	m := NewHealthManager(10, 20)
	m.AddHealth(math.NaN())
	m.SetMaxHealth(math.NaN())
	if m.Health() != 1 || m.MaxHealth() != 1 {
		t.Fatalf("health %v/%v, want 1/1", m.Health(), m.MaxHealth())
	}
	m.SetMaxHealth(math.Inf(1))
	if math.IsInf(m.MaxHealth(), 0) {
		t.Fatal("max health became infinite")
	}
}
