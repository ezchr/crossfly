package player

import (
	"context"
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// spearWorld runs f in a world holding an attacker standing at the origin and looking along +Z.
func spearWorld(t *testing.T, mode world.GameMode, f func(tx *world.Tx, p *Player)) {
	t.Helper()
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		h := world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 0, 0.5}}.New(Type, Config{
			Name: "Attacker", UUID: uuid.New(), GameMode: mode, Health: 20, MaxHealth: 20,
			Position: mgl64.Vec3{0.5, 0, 0.5},
		})
		f(tx, tx.AddEntity(h).(*Player))
		return struct{}{}, nil
	})
}

// addTarget adds a player standing at pos.
func addTarget(tx *world.Tx, pos mgl64.Vec3) *Player {
	h := world.EntitySpawnOpts{Position: pos}.New(Type, Config{
		Name: "Target", UUID: uuid.New(), GameMode: world.GameModeSurvival, Health: 20, MaxHealth: 20, Position: pos,
	})
	return tx.AddEntity(h).(*Player)
}

func jabbed(p *Player, target *Player) bool {
	for _, t := range p.spearJabTargets(item.Spear{Tier: item.ToolTierIron}) {
		if t.e.H() == target.H() {
			return true
		}
	}
	return false
}

// TestSpearJabHitsTargetAtMinReach: a target whose hitbox straddles the minimum reach of 2 blocks is hit,
// as Bedrock starts the jab at the minimum reach and hits a hitbox holding that point. TestPlayer missed
// Ezchr standing about 2.1 blocks away like this.
func TestSpearJabHitsTargetAtMinReach(t *testing.T) {
	spearWorld(t, world.GameModeSurvival, func(tx *world.Tx, p *Player) {
		if target := addTarget(tx, mgl64.Vec3{0.5, 0, 0.5 + 2.1}); !jabbed(p, target) {
			t.Fatal("target 2.1 blocks away, its hitbox around the minimum reach, was not hit")
		}
	})
}

func TestSpearJabReach(t *testing.T) {
	for _, c := range []struct {
		name string
		dz   float64
		hit  bool
	}{
		// The grown hitbox (0.3+0.125 each side) ends before the minimum reach.
		{"too close", 1.5, false},
		{"just inside minimum reach", 1.6, true},
		{"mid reach", 3, true},
		// The grown hitbox starts at 4.375, inside the maximum reach of 4.5.
		{"just inside maximum reach", 4.8, true},
		{"beyond maximum reach", 5, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			spearWorld(t, world.GameModeSurvival, func(tx *world.Tx, p *Player) {
				target := addTarget(tx, mgl64.Vec3{0.5, 0, 0.5 + c.dz})
				if got := jabbed(p, target); got != c.hit {
					t.Fatalf("target %v blocks away: hit %v, want %v", c.dz, got, c.hit)
				}
			})
		})
	}
}

// TestSpearJabHitboxMargin: Bedrock's hitbox_margin of 0.25 grows a hitbox by 0.125 on each side.
func TestSpearJabHitboxMargin(t *testing.T) {
	if m := (item.Spear{Tier: item.ToolTierIron}).HitboxMargin(); m != 0.125 {
		t.Fatalf("hitbox grown by %v each side, want 0.125 (hitbox_margin 0.25)", m)
	}
	for _, c := range []struct {
		dx  float64
		hit bool
	}{{0.4, true}, {0.42, true}, {0.43, false}, {0.5, false}} {
		spearWorld(t, world.GameModeSurvival, func(tx *world.Tx, p *Player) {
			target := addTarget(tx, mgl64.Vec3{0.5 + c.dx, 0, 3.5})
			if got := jabbed(p, target); got != c.hit {
				t.Fatalf("target %v blocks beside the jab: hit %v, want %v", c.dx, got, c.hit)
			}
		})
	}
}

func TestSpearJabBlockedByBlock(t *testing.T) {
	spearWorld(t, world.GameModeSurvival, func(tx *world.Tx, p *Player) {
		tx.SetBlock(cube.Pos{0, 1, 2}, block.Stone{}, nil)
		if target := addTarget(tx, mgl64.Vec3{0.5, 0, 4}); jabbed(p, target) {
			t.Fatal("target behind a block was hit")
		}
	})
}

func holdLunge(p *Player, level int) {
	spear := item.NewStack(item.Spear{Tier: item.ToolTierIron}, 1).WithEnchantments(item.NewEnchantment(enchantment.Lunge, level))
	p.SetHeldItems(spear, item.Stack{})
}

func TestLungeCosts(t *testing.T) {
	spearWorld(t, world.GameModeSurvival, func(tx *world.Tx, p *Player) {
		holdLunge(p, 2)
		p.SetFood(20)
		saturation := p.hunger.saturationLevel
		p.triggerSpearLunge()

		if v := p.Velocity(); math.Abs(v[2]-0.916) > 1e-9 || math.Abs(v[0]) > 1e-9 || v[1] != 0 {
			t.Fatalf("velocity %v after Lunge II, want 0.916 along +Z", v)
		}
		// 8 exhaustion takes 2 saturation points, and no food is taken directly.
		if p.Food() != 20 || p.hunger.saturationLevel != saturation-2 {
			t.Fatalf("food %v, saturation %v after Lunge II, want 20, %v", p.Food(), p.hunger.saturationLevel, saturation-2)
		}
		if held, _ := p.HeldItems(); held.Durability() != held.MaxDurability()-1 {
			t.Fatalf("spear durability %v of %v after Lunge, want one used", held.Durability(), held.MaxDurability())
		}
	})
}

func TestLungeHorizontalOnly(t *testing.T) {
	spearWorld(t, world.GameModeSurvival, func(tx *world.Tx, p *Player) {
		holdLunge(p, 1)
		p.data.Rot = cube.Rotation{0, -60}
		p.triggerSpearLunge()
		if v := p.Velocity(); v[1] != 0 || math.Abs(v[2]-0.458*0.5) > 1e-9 {
			t.Fatalf("velocity %v looking 60 degrees up, want only the horizontal part of the look", v)
		}
	})
}

func TestLungeRequirements(t *testing.T) {
	for _, c := range []struct {
		name  string
		mode  world.GameMode
		food  int
		water bool
		lunge bool
	}{
		{"food 7", world.GameModeSurvival, 7, false, true},
		{"food 6", world.GameModeSurvival, 6, false, false},
		{"creative without food", world.GameModeCreative, 0, false, true},
		{"in water", world.GameModeSurvival, 20, true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			spearWorld(t, c.mode, func(tx *world.Tx, p *Player) {
				holdLunge(p, 1)
				p.SetFood(c.food)
				if c.water {
					tx.SetBlock(cube.Pos{0, 0, 0}, block.Water{Still: true, Depth: 8}, nil)
				}
				p.triggerSpearLunge()
				if got := p.Velocity()[2] > 0; got != c.lunge {
					t.Fatalf("lunged %v, want %v", got, c.lunge)
				}
				if c.mode == world.GameModeCreative {
					if held, _ := p.HeldItems(); held.Durability() != held.MaxDurability() {
						t.Fatal("Lunge used durability in creative")
					}
				}
			})
		})
	}
}

// TestLungeExhaustionRule checks that SetLungeExhaustion replaces Bedrock's 4 exhaustion per level and
// that a negative value brings it back.
func TestLungeExhaustionRule(t *testing.T) {
	defer SetLungeExhaustion(-1)
	for _, c := range []struct{ per, want float64 }{{2, 1}, {0, 0}, {-1, 2}} {
		SetLungeExhaustion(c.per)
		spearWorld(t, world.GameModeSurvival, func(tx *world.Tx, p *Player) {
			holdLunge(p, 2)
			p.SetFood(20)
			saturation := p.hunger.saturationLevel
			p.triggerSpearLunge()
			if got := saturation - p.hunger.saturationLevel; math.Abs(got-c.want) > 1e-9 {
				t.Fatalf("Lunge II at %v per level took %v saturation, want %v", c.per, got, c.want)
			}
		})
	}
}
