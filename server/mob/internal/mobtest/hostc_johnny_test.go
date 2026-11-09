//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestVindicatorJohnny: a name tag "Johnny" renames a vindicator, uses the
// tag up, makes it persistent and fires minecraft:start_johnny
// (nameable name_actions); another name fires default_trigger
// (minecraft:stop_johnny).
func TestVindicatorJohnny(t *testing.T) {
	needs(t, true, true)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	v := w.Spawn("minecraft:vindicator", mgl64.Vec3{0.5, 1, 0.5})
	p := w.AddPlayer("p", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(1)
	johnny := func() bool {
		ok := false
		v.With(func(m *mob.Mob) {
			for _, g := range activeGroups(m) {
				if g == "minecraft:vindicator_johnny" {
					ok = true
				}
			}
		})
		return ok
	}
	left, ok := p.Interact(v, item.NewStack(item.NameTag{}, 2).WithCustomName("Johnny"))
	if !ok || left.Count() != 1 {
		t.Fatalf("name tag not used: ok %v, %d left", ok, left.Count())
	}
	v.With(func(m *mob.Mob) {
		if m.NameTag() != "Johnny" || !m.Persistent() {
			t.Errorf("name %q, persistent %v", m.NameTag(), m.Persistent())
		}
	})
	if !johnny() {
		t.Fatal("Johnny did not fire start_johnny")
	}
	if _, ok := p.Interact(v, item.NewStack(item.NameTag{}, 1).WithCustomName("Johnny")); ok {
		t.Error("renaming to the same name used the tag")
	}
	if _, ok := p.Interact(v, item.NewStack(item.NameTag{}, 1).WithCustomName("Bob")); !ok {
		t.Fatal("renaming to Bob refused")
	}
	if johnny() {
		t.Error("Bob is still Johnny: default_trigger did not fire")
	}
}

// activeGroups lists m's active component groups (mob.InstanceGroups).
func activeGroups(m *mob.Mob) []string {
	if g, ok := m.Instance().(interface{ ActiveGroups() []string }); ok {
		return g.ActiveGroups()
	}
	return nil
}
