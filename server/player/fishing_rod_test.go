package player

import (
	"context"
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// rodWorld runs f in a world with the default entities and a survival player holding a fishing rod, standing at pos
// and looking along rot.
func rodWorld(t *testing.T, pos mgl64.Vec3, rot cube.Rotation, f func(tx *world.Tx, p *Player)) {
	t.Helper()
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		h := world.EntitySpawnOpts{Position: pos, Rotation: rot}.New(Type, Config{
			Name: "Angler", UUID: uuid.New(), GameMode: world.GameModeSurvival, Health: 20, MaxHealth: 20,
			Position: pos, Rotation: rot,
		})
		p := tx.AddEntity(h).(*Player)
		p.SetHeldItems(item.NewStack(item.FishingRod{}, 1), item.Stack{})
		f(tx, p)
		return struct{}{}, nil
	})
}

// hookOf returns the fishing hook p has out in tx.
func hookOf(tx *world.Tx, p *Player) (*entity.Ent, *entity.FishingHookBehaviour) {
	for e := range tx.Entities() {
		if e.H().Type() != entity.FishingHookType {
			continue
		}
		ent := e.(*entity.Ent)
		if b := ent.Behaviour().(*entity.FishingHookBehaviour); b.Owner() == p.H() {
			return ent, b
		}
	}
	return nil, nil
}

// tickHook ticks the hook of p up to n times, until done returns true. It reports whether done did.
func tickHook(tx *world.Tx, p *Player, n int, done func(e *entity.Ent, b *entity.FishingHookBehaviour) bool) bool {
	for i := 0; i < n; i++ {
		e, b := hookOf(tx, p)
		if e == nil {
			return false
		}
		e.Tick(tx, int64(i))
		if e, b = hookOf(tx, p); e != nil && done(e, b) {
			return true
		}
	}
	return false
}

func rodDurability(p *Player) int {
	main, _ := p.HeldItems()
	return main.Durability()
}

func TestFishingRodRegistered(t *testing.T) {
	it, ok := world.ItemByName("minecraft:fishing_rod", 0)
	if !ok {
		t.Fatal("minecraft:fishing_rod is not registered")
	}
	rod, ok := it.(item.FishingRod)
	if !ok {
		t.Fatalf("minecraft:fishing_rod is %T", it)
	}
	if rod.MaxCount() != 1 || rod.DurabilityInfo().MaxDurability != 384 {
		t.Fatalf("max count %d, durability %d; want 1 and 384", rod.MaxCount(), rod.DurabilityInfo().MaxDurability)
	}
	for id, name := range map[int]string{23: "Luck of the Sea", 24: "Lure"} {
		e, ok := item.EnchantmentByID(id)
		if !ok || e.Name() != name || !e.CompatibleWithItem(rod) || e.MaxLevel() != 3 {
			t.Errorf("enchantment %d: %v %v", id, ok, e)
		}
		if lo, hi := e.Cost(1); lo != 15 || hi != 20 {
			t.Errorf("%s I costs %d-%d, want 15-20", name, lo, hi)
		}
	}
	unbreaking, _ := item.EnchantmentByID(17)
	mending, _ := item.EnchantmentByID(26)
	lure, _ := item.EnchantmentByID(24)
	if !unbreaking.CompatibleWithItem(rod) || !mending.CompatibleWithItem(rod) {
		t.Error("unbreaking and mending do not take fishing rods")
	}
	if lure.CompatibleWithItem(item.Sword{Tier: item.ToolTierIron}) {
		t.Error("lure takes a sword")
	}
}

// TestFishingRodCastAndReel: using the rod casts a hook flying along the look direction at 1.5 blocks a tick, using it
// again reels it in for free when it caught nothing and hit nothing.
func TestFishingRodCastAndReel(t *testing.T) {
	rodWorld(t, mgl64.Vec3{0.5, 100, 0.5}, cube.Rotation{0, 0}, func(tx *world.Tx, p *Player) {
		p.UseItem()
		e, b := hookOf(tx, p)
		if e == nil {
			t.Fatal("no hook cast")
		}
		if b.Owner() != p.H() {
			t.Fatal("hook not owned by the caster")
		}
		if v := e.Velocity(); math.Abs(v.Len()-1.5) > 0.1 || v[2] < 1.4 {
			t.Fatalf("cast velocity %v, want about (0, 0, 1.5)", v)
		}
		if want := p.Position().Add(mgl64.Vec3{0, p.EyeHeight()}); !e.Position().ApproxEqual(want) {
			t.Fatalf("hook cast from %v, want the eyes %v", e.Position(), want)
		}
		e.Tick(tx, 0)
		p.UseItem()
		if e, _ := hookOf(tx, p); e != nil {
			t.Fatal("hook not reeled in")
		}
		if d := rodDurability(p); d != 384 {
			t.Fatalf("durability %d after reeling in nothing, want 384", d)
		}
		p.UseItem()
		if e, _ := hookOf(tx, p); e == nil {
			t.Fatal("no hook cast the second time")
		}
	})
}

// TestFishingRodGround: a hook that hit a block costs 2 durability.
func TestFishingRodGround(t *testing.T) {
	rodWorld(t, mgl64.Vec3{0.5, 70, 0.5}, cube.Rotation{0, 90}, func(tx *world.Tx, p *Player) {
		for x := -3; x <= 3; x++ {
			for z := -3; z <= 3; z++ {
				tx.SetBlock(cube.Pos{x, 60, z}, block.Stone{}, nil)
			}
		}
		p.UseItem()
		if !tickHook(tx, p, 40, func(_ *entity.Ent, b *entity.FishingHookBehaviour) bool { return b.InGround() }) {
			t.Fatal("hook never hit the ground")
		}
		e, _ := hookOf(tx, p)
		for i := 0; i < 20; i++ {
			e.Tick(tx, int64(i))
		}
		if y := e.Position()[1]; y < 61 || y > 61.2 {
			t.Fatalf("hook rests at y %v, want on the stone at 61", y)
		}
		p.UseItem()
		if d := rodDurability(p); d != 384-entity.FishingRodGroundDurability {
			t.Fatalf("durability %d after reeling in from the ground, want %d", d, 384-entity.FishingRodGroundDurability)
		}
	})
}

// TestFishingRodHookPlayer: the hook catches a player in front, hurts it for nothing but knocks it back, rides on it,
// and reeling in pulls it towards the angler for 3 durability.
func TestFishingRodHookPlayer(t *testing.T) {
	rodWorld(t, mgl64.Vec3{0.5, 100, 0.5}, cube.Rotation{0, 0}, func(tx *world.Tx, p *Player) {
		target := addTarget(tx, mgl64.Vec3{0.5, 100, 4.5})
		p.UseItem()
		if !tickHook(tx, p, 10, func(_ *entity.Ent, b *entity.FishingHookBehaviour) bool { return b.TargetEntity() != nil }) {
			t.Fatal("the target in front was not hooked")
		}
		e, b := hookOf(tx, p)
		if b.TargetEntity() != target.H() {
			t.Fatal("hooked the wrong entity")
		}
		if target.Health() != 20 {
			t.Fatalf("hooked target has %v health, want 20", target.Health())
		}
		if v := target.Velocity(); v[2] <= 0 || v[1] <= 0 {
			t.Fatalf("hooked target velocity %v, want knocked away (+Z) and up", v)
		}
		if target.immuneTicks == 0 {
			t.Fatal("the hook's hit gave no hurt immunity")
		}
		target.SetVelocity(mgl64.Vec3{})
		e.Tick(tx, 20)
		want := target.Position().Add(mgl64.Vec3{0, target.H().Type().BBox(target).Height() * 0.8})
		if !e.Position().ApproxEqual(want) {
			t.Fatalf("hook at %v, want on the target at %v", e.Position(), want)
		}

		p.UseItem()
		if e, _ := hookOf(tx, p); e != nil {
			t.Fatal("hook not reeled in")
		}
		if d := rodDurability(p); d != 384-entity.FishingRodHookedDurability {
			t.Fatalf("durability %d after reeling in a player, want %d", d, 384-entity.FishingRodHookedDurability)
		}
		// _pullCloser(target, 0.1): 0.1 of the way from the target's eyes to 1 above the angler's eyes, plus 0.08 of
		// that upwards.
		d := p.Position().Add(mgl64.Vec3{0, p.EyeHeight() + 1}).Sub(target.Position().Add(mgl64.Vec3{0, target.EyeHeight()})).Mul(0.1)
		d[1] += d.Len() * 0.08
		if v := target.Velocity(); v.Sub(d).Len() > 1e-9 {
			t.Fatalf("pulled with %v, want %v", v, d)
		}
		if target.Velocity()[2] >= 0 {
			t.Fatal("target not pulled towards the angler")
		}
	})
}

// TestFishingHookRemoved: the hook goes when its owner stops holding the rod, goes too far, dies or leaves.
func TestFishingHookRemoved(t *testing.T) {
	cases := map[string]func(tx *world.Tx, p *Player){
		"switched item": func(_ *world.Tx, p *Player) { p.SetHeldItems(item.NewStack(item.Stick{}, 1), item.Stack{}) },
		"rod in off hand": func(_ *world.Tx, p *Player) {
			p.SetHeldItems(item.Stack{}, item.NewStack(item.FishingRod{}, 1))
		},
		"too far":   func(_ *world.Tx, p *Player) { p.Teleport(p.Position().Add(mgl64.Vec3{40, 0, 0})) },
		"died":      func(_ *world.Tx, p *Player) { p.Hurt(100, entity.VoidDamageSource{}) },
		"left":      func(tx *world.Tx, p *Player) { tx.RemoveEntity(p) },
		"stays put": func(*world.Tx, *Player) {},
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			rodWorld(t, mgl64.Vec3{0.5, 100, 0.5}, cube.Rotation{0, 0}, func(tx *world.Tx, p *Player) {
				p.UseItem()
				e, _ := hookOf(tx, p)
				f(tx, p)
				e.Tick(tx, 0)
				_, there := e.H().Entity(tx)
				if want := name == "stays put"; there != want {
					t.Fatalf("hook still there: %v, want %v", there, want)
				}
				if name != "stays put" && name != "left" {
					if d, ok := entity.ReelFishingHook(tx, p); ok {
						t.Fatalf("a removed hook was reeled in (%d)", d)
					}
				}
			})
		})
	}
}
