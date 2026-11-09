package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// gliding puts an elytra on p and starts its glide.
func gliding(t *testing.T, p *Player) {
	t.Helper()
	p.Armour().SetChestplate(item.NewStack(item.Elytra{}, 1))
	if p.StartGliding(); !p.Gliding() {
		t.Fatal("a player wearing an elytra did not start gliding")
	}
}

// TestGlideEndsWithoutElytra: a glide ends on the next tick once the chest slot no longer holds a
// usable elytra, however it went (Ezchr kept gliding after another player took his elytra out of
// the slot with /invsee). Java clients never say they stopped: the server ends it, as vanilla's
// LivingEntity.updateFallFlying does.
func TestGlideEndsWithoutElytra(t *testing.T) {
	for _, c := range []struct {
		name  string
		chest item.Stack
	}{
		{"taken out", item.Stack{}},
		{"swapped for a chestplate", item.NewStack(item.Chestplate{Tier: item.ArmourTierIron{}}, 1)},
		{"about to break", item.NewStack(item.Elytra{}, 1).WithDurability(1)},
	} {
		t.Run(c.name, func(t *testing.T) {
			spearWorld(t, world.GameModeSurvival, func(tx *world.Tx, p *Player) {
				gliding(t, p)
				p.Tick(tx, 1)
				if !p.Gliding() {
					t.Fatal("the glide ended with the elytra still worn")
				}
				p.Armour().SetChestplate(c.chest)
				p.Tick(tx, 2)
				if p.Gliding() {
					t.Fatal("still gliding after the elytra went")
				}
			})
		})
	}
}

// TestGlideEndsOnDeathAndTeleport: dying ends a glide (a player who died while stuck gliding glided on
// after the respawn), a teleport in the same world does not, as in vanilla.
func TestGlideEndsOnDeathAndTeleport(t *testing.T) {
	spearWorld(t, world.GameModeSurvival, func(tx *world.Tx, p *Player) {
		gliding(t, p)
		p.Teleport(mgl64.Vec3{0.5, 10, 0.5})
		if !p.Gliding() {
			t.Fatal("a teleport ended the glide")
		}
	})
	spearWorld(t, world.GameModeSurvival, func(tx *world.Tx, p *Player) {
		gliding(t, p)
		p.Hurt(1000, entity.VoidDamageSource{})
		if !p.Dead() {
			t.Fatal("the player did not die")
		}
		if p.Gliding() {
			t.Fatal("still gliding after dying")
		}
	})
}
