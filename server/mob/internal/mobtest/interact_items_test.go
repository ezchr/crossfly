//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/loot"
	"github.com/df-mc/dragonfly/server/mob/component/ride"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// interactWorld is a stone floor with a viewer recording particles and
// sounds.
func interactWorld(t *testing.T) (*mobtest.World, *eaViewer) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 12, block.Stone{}) })
	v := &eaViewer{}
	w.Do(func(tx *world.Tx) {
		l := world.NewLoader(4, tx.World(), v)
		l.Move(tx, mgl64.Vec3{0.5, 1, 0.5})
		l.Load(tx, 1000)
	})
	return w, v
}

// inventoryHas counts the items named name in the player's inventory.
func inventoryHas(p *mobtest.Player, name string) (n int) {
	p.With(func(pl *player.Player) {
		for _, s := range pl.Inventory().Items() {
			if got, _ := s.Item().EncodeItem(); got == name {
				n += s.Count()
			}
		}
	})
	return n
}

// TestMooshroomStew: a bowl on an adult mooshroom gives mushroom stew
// (add_items mooshroom_milking.json, mark variant -1). One bowl turns into
// the stew in the hand; of three, one is used and the stew goes to the
// inventory.
func TestMooshroomStew(t *testing.T) {
	needs(t, true, true, "minecraft:interact", "minecraft:mark_variant")
	w, _ := interactWorld(t)
	mo := w.Spawn("minecraft:mooshroom", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:become_red_adult"})
	p := w.AddPlayer("p", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(2)
	left, ok := p.Interact(mo, item.NewStack(item.Bowl{}, 1))
	if !ok {
		t.Fatalf("interaction refused:\n%v", w.Dump(mo))
	}
	if _, stew := left.Item().(item.MushroomStew); !stew || left.Count() != 1 {
		t.Fatalf("hand holds %v, want one mushroom stew", left)
	}
	left, ok = p.Interact(mo, item.NewStack(item.Bowl{}, 3))
	if !ok {
		t.Fatalf("second interaction refused")
	}
	if _, bowl := left.Item().(item.Bowl); !bowl || left.Count() != 2 {
		t.Errorf("hand holds %v, want 2 bowls", left)
	}
	if n := inventoryHas(p, "minecraft:mushroom_stew"); n != 1 {
		t.Errorf("inventory has %d mushroom stews besides the hand, want 1", n)
	}
}

// TestBrownMooshroomSuspiciousStew: feeding a brown mooshroom a poppy uses
// the poppy, remembers it as mark variant 0 and shows smoke towards the
// player (particle_on_start); the next bowl gives a night vision suspicious
// stew and makes it flowerless again, so the bowl after gives plain stew.
func TestBrownMooshroomSuspiciousStew(t *testing.T) {
	needs(t, true, true, "minecraft:interact", "minecraft:mark_variant")
	w, v := interactWorld(t)
	mo := w.Spawn("minecraft:mooshroom", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:become_brown_adult"})
	p := w.AddPlayer("p", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(2)
	v.particles = nil
	left, ok := p.Interact(mo, item.NewStack(block.Flower{Type: block.Poppy()}, 2))
	if !ok {
		t.Fatalf("feeding refused:\n%v", w.Dump(mo))
	}
	if left.Count() != 1 {
		t.Errorf("%d poppies left, want 1", left.Count())
	}
	mo.With(func(m *mob.Mob) {
		if mv := m.Meta().MarkVariant; mv != 0 {
			t.Errorf("mark variant %d after a poppy, want 0", mv)
		}
	})
	smoke := false
	for _, pa := range v.particles {
		if l, ok := pa.(particle.Legacy); ok && l.Bedrock == packet.ParticleTypeSmoke {
			smoke = true
		}
	}
	if !smoke {
		t.Errorf("no smoke particle on feeding: %v", v.particles)
	}
	left, _ = p.Interact(mo, item.NewStack(item.Bowl{}, 1))
	st, ok := left.Item().(item.SuspiciousStew)
	if !ok || st.Type != item.NightVisionPoppyStew() {
		t.Fatalf("hand holds %v, want a night vision (poppy) suspicious stew", left)
	}
	mo.With(func(m *mob.Mob) {
		if mv := m.Meta().MarkVariant; mv != -1 {
			t.Errorf("mark variant %d after milking, want -1", mv)
		}
	})
	left, _ = p.Interact(mo, item.NewStack(item.Bowl{}, 1))
	if _, stew := left.Item().(item.MushroomStew); !stew {
		t.Errorf("second bowl gives %v, want mushroom stew", left)
	}
}

// TestAllayGiveTake: a player gives an allay the held item (one of it, the
// allay likes the player) and takes it back with an empty hand (the allay
// forgets the player).
func TestAllayGiveTake(t *testing.T) {
	needs(t, true, true, "minecraft:interact")
	w, _ := interactWorld(t)
	al := w.Spawn("minecraft:allay", mgl64.Vec3{0.5, 1, 0.5})
	p := w.AddPlayer("p", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(2)
	left, ok := p.Interact(al, item.NewStack(item.Cookie{}, 5))
	if !ok {
		t.Fatalf("give refused:\n%v", w.Dump(al))
	}
	if left.Count() != 4 {
		t.Errorf("%d cookies left, want 4", left.Count())
	}
	al.With(func(m *mob.Mob) {
		main, _ := m.HeldItems()
		if _, ok := main.Item().(item.Cookie); !ok || main.Count() != 1 {
			t.Errorf("allay holds %v, want one cookie", main)
		}
		if !loot.WasPickedUp(main) {
			t.Errorf("given cookie not marked picked up")
		}
		if o := m.Owner(); o == nil || o != p.H() {
			t.Errorf("allay likes %v, want the player", o)
		}
	})
	// A second item is not given while the allay holds one.
	if _, ok := p.Interact(al, item.NewStack(item.Apple{}, 1)); ok {
		t.Errorf("allay holding a cookie took an apple")
	}
	p.Hold(item.Stack{})
	if _, ok := p.Interact(al, item.Stack{}); !ok {
		t.Fatalf("take refused")
	}
	if n := inventoryHas(p, "minecraft:cookie"); n != 1 {
		t.Errorf("player got %d cookies back, want 1", n)
	}
	p.With(func(pl *player.Player) {
		for _, s := range pl.Inventory().Items() {
			if loot.WasPickedUp(s) {
				t.Errorf("returned %v still marked picked up", s)
			}
		}
	})
	al.With(func(m *mob.Mob) {
		if main, _ := m.HeldItems(); !main.Empty() {
			t.Errorf("allay still holds %v", main)
		}
		if o := m.Owner(); o != nil {
			t.Errorf("allay still likes %v", o)
		}
	})
}

// TestMooshroomShearEffects: shearing a red mooshroom shows the
// largeexplode particle and emits the shear vibration (particle_on_start,
// vibration), besides its spawn_items and become_cow event.
func TestMooshroomShearEffects(t *testing.T) {
	needs(t, true, true, "minecraft:interact")
	w, v := interactWorld(t)
	mo := w.Spawn("minecraft:mooshroom", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:become_red_adult"})
	p := w.AddPlayer("p", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(2)
	v.particles, vibrations = nil, nil
	if _, ok := p.Interact(mo, item.NewStack(item.Shears{}, 1)); !ok {
		t.Fatalf("shearing refused:\n%v", w.Dump(mo))
	}
	explode := false
	for _, pa := range v.particles {
		if l, ok := pa.(particle.Legacy); ok && l.Bedrock == packet.ParticleTypeLargeExplode {
			explode = true
		}
	}
	if !explode {
		t.Errorf("no largeexplode particle: %v", v.particles)
	}
	found := false
	for _, e := range vibrations {
		if e == "minecraft:mooshroom shear" {
			found = true
		}
	}
	if !found {
		t.Errorf("no shear vibration: %v", vibrations)
	}
}

// TestLlamaCarpetSlot: the llama's interactions name its carpet slot "0"
// (equip_item_slot, drop_item_slot): the first equippable slot, slot 1
// (Interaction::_tryConvertStringToItemSlotInfo reads container slot 0).
// The interactions themselves do not run yet: their has_equipment filters
// ask for "carpet", which package filter compares with the coloured carpet
// names literally.
func TestLlamaCarpetSlot(t *testing.T) {
	needs(t, true, true, "minecraft:interact", "minecraft:equippable")
	w, _ := interactWorld(t)
	ll := w.Spawn("minecraft:llama", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_tame_adult"})
	w.Tick(2)
	ll.With(func(m *mob.Mob) {
		e, ok := mob.ComponentOf[*ride.Equippable](m)
		if !ok {
			t.Fatalf("tamed llama has no equippable")
		}
		if !m.EquipSlot("0", item.NewStack(block.Carpet{Colour: item.ColourRed()}, 2)) {
			t.Fatalf("carpet not equipped into slot 0")
		}
		if s := e.Item(1); s.Count() != 1 {
			t.Fatalf("slot 1 holds %v, want one carpet", s)
		}
		s, ok := m.DropSlot("0")
		if _, carpet := s.Item().(block.Carpet); !ok || !carpet {
			t.Errorf("slot 0 dropped %v, %v; want the carpet", s, ok)
		}
		if !e.Item(1).Empty() {
			t.Errorf("llama still wears %v", e.Item(1))
		}
	})
}
