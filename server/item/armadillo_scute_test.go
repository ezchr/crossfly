package item_test

import (
	"sync"
	"testing"
	_ "unsafe"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/creative"
	"github.com/df-mc/dragonfly/server/java/javamap"
	jitem "github.com/df-mc/dragonfly/server/java/protocol/item"
	"github.com/df-mc/dragonfly/server/world"
)

//go:linkname pbRegisterCreative github.com/df-mc/dragonfly/server/item/creative.registerCreativeItems
func pbRegisterCreative()

var (
	pbCreativeOnce sync.Once
	pbCreative     map[string]bool
)

// pbInCreative reports whether the creative inventory holds the item called name. The creative inventory is
// filled once, after the init functions of the test binary registered their items.
func pbInCreative(name string) bool {
	pbCreativeOnce.Do(func() {
		world.DefaultBlockRegistry.Finalize()
		pbRegisterCreative()
		pbCreative = map[string]bool{}
		for _, c := range creative.Items() {
			n, _ := c.Stack.Item().EncodeItem()
			pbCreative[n] = true
		}
	})
	return pbCreative[name]
}

// pbCheckItem checks that it is registered under name, stacks to maxCount, is in the creative inventory and is
// the Java item javaName for Java clients.
func pbCheckItem(t *testing.T, it world.Item, name, javaName string, maxCount int) {
	t.Helper()
	n, meta := it.EncodeItem()
	if n != name {
		t.Errorf("%T encodes as %v, want %v", it, n, name)
	}
	if got, ok := world.ItemByName(n, meta); !ok || got != it {
		t.Errorf("%v not registered (got %v)", name, got)
	}
	count := 64
	if c, ok := it.(item.MaxCounter); ok {
		count = c.MaxCount()
	}
	if count != maxCount {
		t.Errorf("%v stacks to %d, want %d", name, count, maxCount)
	}
	if !pbInCreative(name) {
		t.Errorf("%v not in the creative inventory", name)
	}
	if got := jitem.Name(javamap.Item(it)); got != javaName {
		t.Errorf("%v is Java item %v, want %v", name, got, javaName)
	}
}

// TestArmadilloScuteAndBrush checks the armadillo scute and the brush items for both editions.
func TestArmadilloScuteAndBrush(t *testing.T) {
	pbCheckItem(t, item.ArmadilloScute{}, "minecraft:armadillo_scute", "minecraft:armadillo_scute", 64)
	pbCheckItem(t, item.Brush{}, "minecraft:brush", "minecraft:brush", 1)

	s := item.NewStack(item.Brush{}, 1)
	if s.MaxDurability() != 64 || s.Durability() != 64 {
		t.Fatalf("brush durability %v/%v, want 64/64", s.Durability(), s.MaxDurability())
	}
	if s = s.Damage(16); s.Durability() != 48 {
		t.Errorf("brush brushed once has %v durability, want 48", s.Durability())
	}
	if s = s.Damage(48); !s.Empty() {
		t.Errorf("brush with no durability left is %v, want gone", s)
	}
}
