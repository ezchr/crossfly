//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
)

// golemUser is an item.User holding one item.
type golemUser struct {
	world.Entity
	held item.Stack
}

func (u *golemUser) HeldItems() (item.Stack, item.Stack) { return u.held, item.Stack{} }
func (u *golemUser) SetHeldItems(main, _ item.Stack)     { u.held = main }
func (*golemUser) UsingItem() bool                       { return false }
func (*golemUser) ReleaseItem()                          {}
func (*golemUser) UseItem()                              {}

func TestAxeTurnsStatueBackIntoCopperGolem(t *testing.T) {
	needs(t, true, true)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 6, block.Stone{}) })
	w.Do(func(tx *world.Tx) {
		pos := cube.Pos{2, 1, 2}
		st := block.CopperGolemStatue{Facing: cube.North}
		tx.SetBlock(pos, st, nil)
		ctx := &item.UseContext{}
		if !st.Activate(pos, cube.FaceUp, tx, &golemUser{held: item.NewStack(item.Axe{Tier: item.ToolTierIron}, 1)}, ctx) {
			t.Fatal("axe did not turn the statue into a golem")
		}
	})
	w.Tick(1)
	golems := w.Mobs("minecraft:copper_golem")
	if len(golems) != 1 {
		t.Fatalf("%d copper golems, want 1", len(golems))
	}
	golems[0].With(func(m *mob.Mob) {
		if v, _ := m.Property("minecraft:oxidation_level"); v != "unoxidized" {
			t.Fatalf("revived golem oxidation %v, want unoxidized", v)
		}
		if v, _ := m.Property("minecraft:is_becoming_statue"); v != false {
			t.Fatalf("revived golem is_becoming_statue %v", v)
		}
		if g, ok := m.Instance().(interface{ HasGroup(string) bool }); !ok || !g.HasGroup("minecraft:copper_oxidizing") {
			t.Fatal("revived golem does not oxidise again")
		}
	})
}
