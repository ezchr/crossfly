package block

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// golemStatueUser is an item.User holding one item.
type golemStatueUser struct {
	world.Entity
	held item.Stack
}

func (u *golemStatueUser) HeldItems() (item.Stack, item.Stack) { return u.held, item.Stack{} }
func (u *golemStatueUser) SetHeldItems(main, _ item.Stack)     { u.held = main }
func (*golemStatueUser) UsingItem() bool                       { return false }
func (*golemStatueUser) ReleaseItem()                          {}
func (*golemStatueUser) UseItem()                              {}

func TestAxeRevivesCopperGolemStatue(t *testing.T) {
	h := withGolemHost(t)
	golemWorld(t, func(tx *world.Tx) {
		pos := cube.Pos{2, 4, 6}
		tx.SetBlock(pos, CopperGolemStatue{Facing: cube.East, Pose: StarPose()}, nil)
		ctx := &item.UseContext{}
		u := &golemStatueUser{held: item.NewStack(item.Axe{Tier: item.ToolTierIron}, 1)}
		if !tx.Block(pos).(CopperGolemStatue).Activate(pos, cube.FaceUp, tx, u, ctx) {
			t.Fatal("axe did not revive the statue")
		}
		s := golemOnlySpawn(t, h, "minecraft:copper_golem")
		// At the statue's block, centred, looking where the statue looked (east: yaw -90).
		if !golemNear(s.pos, mgl64.Vec3{2.5, 4, 6.5}) || math.Abs(s.opts.Yaw+90) > 1e-9 || s.opts.Event != "minecraft:from_serialized_entity" {
			t.Fatalf("golem %+v, want (2.5, 4, 6.5) yaw -90 from_serialized_entity", s)
		}
		golemRequireAir(t, tx, pos)
		if ctx.Damage != 1 {
			t.Fatalf("axe damage %d, want 1", ctx.Damage)
		}
	})
}

func TestAxeLeavesWaxedAndOxidisedStatuesToStripping(t *testing.T) {
	h := withGolemHost(t)
	golemWorld(t, func(tx *world.Tx) {
		u := &golemStatueUser{held: item.NewStack(item.Axe{Tier: item.ToolTierStone}, 1)}
		for i, st := range []CopperGolemStatue{{Waxed: true}, {Oxidation: ExposedOxidation()}, {Oxidation: OxidisedOxidation(), Waxed: true}} {
			pos := cube.Pos{i, 1, 0}
			tx.SetBlock(pos, st, nil)
			ctx := &item.UseContext{}
			if st.Activate(pos, cube.FaceUp, tx, u, ctx) {
				t.Fatalf("%+v: axe revived the statue", st)
			}
			// The axe then strips it as before: wax off, or one level of oxidation off.
			if _, _, ok := st.Strip(); !ok {
				t.Fatalf("%+v: statue no longer strippable", st)
			}
		}
		if len(h.spawns) != 0 {
			t.Fatalf("golems spawned: %+v", h.spawns)
		}
	})
}

func TestStatueKeepsPosingWithEmptyHandAndWithoutMobs(t *testing.T) {
	golemWorld(t, func(tx *world.Tx) {
		pos := cube.Pos{0, 1, 0}
		tx.SetBlock(pos, CopperGolemStatue{}, nil)
		axe := &golemStatueUser{held: item.NewStack(item.Axe{Tier: item.ToolTierIron}, 1)}
		old := mobHost
		mobHost = nil
		revived := tx.Block(pos).(CopperGolemStatue).Activate(pos, cube.FaceUp, tx, axe, &item.UseContext{})
		mobHost = old
		if revived {
			t.Fatal("statue revived without mobs")
		}
		if !tx.Block(pos).(CopperGolemStatue).Activate(pos, cube.FaceUp, tx, &golemStatueUser{}, &item.UseContext{}) {
			t.Fatal("empty hand did not pose the statue")
		}
		if p := tx.Block(pos).(CopperGolemStatue).Pose; p != SittingPose() {
			t.Fatalf("pose %v, want sitting", p.Name())
		}
	})
}
