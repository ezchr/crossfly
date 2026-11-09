package enchantment_test

import (
	"io"
	"slices"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// familyTarget is a stand-in for a mob: an entity with type families that
// takes effects.
type familyTarget struct {
	families []string
	dead     bool
	effects  []effect.Effect
}

func (*familyTarget) Close() error              { return io.EOF }
func (*familyTarget) H() *world.EntityHandle    { return nil }
func (*familyTarget) Position() mgl64.Vec3      { return mgl64.Vec3{} }
func (*familyTarget) Rotation() cube.Rotation   { return cube.Rotation{} }
func (f *familyTarget) HasFamily(s string) bool { return slices.Contains(f.families, s) }
func (f *familyTarget) Dead() bool              { return f.dead }
func (f *familyTarget) AddEffect(e effect.Effect) {
	f.effects = append(f.effects, e)
}

// plainTarget has no type families, like a player.
type plainTarget struct{ familyTarget }

func (*plainTarget) HasFamily(string) bool { return false }

func sword(e ...item.Enchantment) item.Stack {
	return item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1).WithEnchantments(e...)
}

func TestSmiteAndBaneRegistered(t *testing.T) {
	for id, want := range map[int]item.EnchantmentType{10: enchantment.Smite, 11: enchantment.BaneOfArthropods} {
		if got, ok := item.EnchantmentByID(id); !ok || got != want {
			t.Errorf("enchantment %d = %v, want %v", id, got, want.Name())
		}
	}
}

func TestDamageEnchantmentsExcludeEachOther(t *testing.T) {
	damage := []item.EnchantmentType{enchantment.Sharpness, enchantment.Smite, enchantment.BaneOfArthropods}
	for _, a := range damage {
		for _, b := range damage {
			if a != b && a.CompatibleWithEnchantment(b) {
				t.Errorf("%v is compatible with %v", a.Name(), b.Name())
			}
		}
		if !a.CompatibleWithEnchantment(enchantment.FireAspect) || !a.CompatibleWithEnchantment(enchantment.Knockback) {
			t.Errorf("%v is not compatible with Fire Aspect or Knockback", a.Name())
		}
		for _, it := range []world.Item{item.Sword{Tier: item.ToolTierIron}, item.Axe{Tier: item.ToolTierIron}} {
			if !a.CompatibleWithItem(it) {
				t.Errorf("%v does not apply to %T", a.Name(), it)
			}
		}
		if a.CompatibleWithItem(item.Pickaxe{Tier: item.ToolTierIron}) {
			t.Errorf("%v applies to a pickaxe", a.Name())
		}
	}
}

func TestSmiteAndBaneCosts(t *testing.T) {
	for _, e := range []item.EnchantmentType{enchantment.Smite, enchantment.BaneOfArthropods} {
		if e.MaxLevel() != 5 || e.Rarity() != item.EnchantmentRarityUncommon {
			t.Errorf("%v: max level %d, rarity %v", e.Name(), e.MaxLevel(), e.Rarity().Name())
		}
		for level, want := range map[int][2]int{1: {5, 25}, 5: {37, 57}, 10: {77, 97}} {
			if lo, hi := e.Cost(level); lo != want[0] || hi != want[1] {
				t.Errorf("%v %d cost = %d-%d, want %d-%d", e.Name(), level, lo, hi, want[0], want[1])
			}
		}
	}
}

func TestSmiteAndBaneAboveMaxLevel(t *testing.T) {
	approx(t, "Smite 1", enchantment.Smite.Addend(1), 2.5)
	approx(t, "Smite 5", enchantment.Smite.Addend(5), 12.5)
	approx(t, "Smite 10", enchantment.Smite.Addend(10), 25)
	approx(t, "Smite 255", enchantment.Smite.Addend(255), 637.5)
	approx(t, "Bane of Arthropods 5", enchantment.BaneOfArthropods.Addend(5), 12.5)
	approx(t, "Bane of Arthropods 255", enchantment.BaneOfArthropods.Addend(255), 637.5)
	// Sharpness is unchanged.
	approx(t, "Sharpness 5", enchantment.Sharpness.Addend(5), 6.25)
}

func TestTargetDamageBonus(t *testing.T) {
	zombie := &familyTarget{families: []string{"zombie", "undead", "monster", "mob"}}
	spider := &familyTarget{families: []string{"spider", "arthropod", "monster", "mob"}}
	cow := &familyTarget{families: []string{"cow", "mob"}}
	player := &plainTarget{}
	smite := sword(item.NewEnchantment(enchantment.Smite, 3))
	bane := sword(item.NewEnchantment(enchantment.BaneOfArthropods, 10))
	sharp := sword(item.NewEnchantment(enchantment.Sharpness, 5))
	for _, c := range []struct {
		name   string
		held   item.Stack
		target world.Entity
		want   float64
	}{
		{"smite vs zombie", smite, zombie, 7.5},
		{"smite vs spider", smite, spider, 0},
		{"smite vs cow", smite, cow, 0},
		{"smite vs player", smite, player, 0},
		{"smite vs nil", smite, nil, 0},
		{"bane 10 vs spider", bane, spider, 25},
		{"bane vs zombie", bane, zombie, 0},
		{"bane vs player", bane, player, 0},
		{"sharpness vs zombie", sharp, zombie, 0},
		{"bare sword vs zombie", sword(), zombie, 0},
	} {
		approx(t, c.name, enchantment.TargetDamageBonus(c.held, c.target), c.want)
	}
}

func TestBaneSlownessDuration(t *testing.T) {
	for _, level := range []int{1, 5, 10, 255} {
		lo, hi := time.Second, time.Second+time.Duration(10*level-1)*time.Second/20
		seenLo, seenHi := false, false
		for range 20000 {
			d := enchantment.BaneOfArthropods.SlownessDuration(level)
			if d < lo || d > hi || d%(time.Second/20) != 0 {
				t.Fatalf("Bane of Arthropods %d: slowness %v, want whole ticks in [%v, %v]", level, d, lo, hi)
			}
			seenLo, seenHi = seenLo || d == lo, seenHi || d == hi
		}
		if level <= 10 && (!seenLo || !seenHi) {
			t.Errorf("Bane of Arthropods %d: never rolled %v (%v) or %v (%v)", level, lo, seenLo, hi, seenHi)
		}
	}
}

func TestSlowArthropod(t *testing.T) {
	bane := sword(item.NewEnchantment(enchantment.BaneOfArthropods, 2))

	spider := &familyTarget{families: []string{"spider", "arthropod"}}
	enchantment.SlowArthropod(bane, spider)
	if len(spider.effects) != 1 {
		t.Fatalf("spider got %d effects, want 1", len(spider.effects))
	}
	e := spider.effects[0]
	if e.Type() != effect.Slowness || e.Level() != 1 {
		t.Errorf("spider got %T level %d, want Slowness I", e.Type(), e.Level())
	}
	if e.Duration() < time.Second || e.Duration() > time.Second+19*time.Second/20 {
		t.Errorf("spider slowed for %v, want 1s to 1.95s", e.Duration())
	}

	for name, target := range map[string]*familyTarget{
		"zombie":      {families: []string{"zombie", "undead"}},
		"dead spider": {families: []string{"arthropod"}, dead: true},
	} {
		enchantment.SlowArthropod(bane, target)
		if len(target.effects) != 0 {
			t.Errorf("%s got %d effects, want none", name, len(target.effects))
		}
	}
	player := &plainTarget{}
	enchantment.SlowArthropod(bane, player)
	if len(player.effects) != 0 {
		t.Errorf("player got %d effects, want none", len(player.effects))
	}
	other := &familyTarget{families: []string{"arthropod"}}
	enchantment.SlowArthropod(sword(item.NewEnchantment(enchantment.Smite, 5)), other)
	if len(other.effects) != 0 {
		t.Errorf("smite slowed an arthropod")
	}
}
