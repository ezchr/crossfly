//go:build crossfly_full

package mobtest_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestMobArmour: Mob::getArmorValue (worn armour plus the natural armour of
// zombies) and Mob::getDamageAfterArmorReduction lower the damage mobs take
// from armour-reduced sources, and the worn pieces lose durability
// (Mob::hurtArmorSlots: max(damage/4, 1)).
func TestMobArmour(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 12, block.Stone{}) })
	sk := w.Spawn("minecraft:skeleton", mgl64.Vec3{0.5, 1, 0.5})
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{4.5, 1, 0.5})
	zd := w.Spawn("minecraft:zombie", mgl64.Vec3{-4.5, 1, 0.5})
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-6 }
	w.Do(func(tx *world.Tx) {
		skm, _ := sk.Mob(tx)
		zm, _ := z.Mob(tx)
		zdm, _ := zd.Mob(tx)
		for _, m := range []interface{ SetMaxHealth(float64) }{skm, zm, zdm} {
			m.SetMaxHealth(40)
		}
		skm.SetHealth(40)
		zm.SetHealth(40)
		zdm.SetHealth(40)
		chest := item.NewStack(item.Chestplate{Tier: item.ArmourTierIron{}}, 1)
		skm.Armour().SetChestplate(chest)
		zdm.Armour().Set(
			item.NewStack(item.Helmet{Tier: item.ArmourTierDiamond{}}, 1),
			item.NewStack(item.Chestplate{Tier: item.ArmourTierDiamond{}}, 1),
			item.NewStack(item.Leggings{Tier: item.ArmourTierDiamond{}}, 1),
			item.NewStack(item.Boots{Tier: item.ArmourTierDiamond{}}, 1),
		)
		if v := skm.ArmourValue(); v != 6 {
			t.Errorf("skeleton with an iron chestplate: armour %v, want 6", v)
		}
		if v := zm.ArmourValue(); v != 2 {
			t.Errorf("bare zombie: armour %v, want 2", v)
		}
		if v := zdm.ArmourValue(); v != 20 {
			t.Errorf("zombie in diamond: armour %v, want 20 (capped)", v)
		}
		src := entity.AttackDamageSource{}
		// Iron chestplate: max(6-10/2, 1.2) = 1.2 points, 4.8% off.
		if n, _ := skm.Hurt(10, src); !near(n, 9.52) {
			t.Errorf("skeleton took %v of 10, want 9.52", n)
		}
		// Natural 2: max(2-5, 0.4) = 0.4 points.
		if n, _ := zm.Hurt(10, src); !near(n, 9.84) {
			t.Errorf("zombie took %v of 10, want 9.84", n)
		}
		// Diamond, toughness 8: 20-10/4 = 17.5 points, 70% off.
		if n, _ := zdm.Hurt(10, src); !near(n, 3) {
			t.Errorf("diamond zombie took %v of 10, want 3", n)
		}
		if d := skm.Armour().Chestplate().Durability(); d != chest.Durability()-2 {
			t.Errorf("chestplate durability %v, want %v", d, chest.Durability()-2)
		}
	})
	w.Tick(15) // past the hurt cooldown
	w.Do(func(tx *world.Tx) {
		skm, _ := sk.Mob(tx)
		before := skm.Health()
		// Fall damage is not reduced by armour.
		if n, _ := skm.Hurt(4, entity.FallDamageSource{}); !near(n, 4) || !near(before-skm.Health(), 4) {
			t.Errorf("fall damage of 4 on an armoured skeleton dealt %v", n)
		}
	})
}
