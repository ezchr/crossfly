package warps

import (
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestEverySubcommandOverload(t *testing.T) {
	testWorld(t, func(m *Manager, tx *world.Tx, p *player.Player) {
		m.config.HasTag = func(*player.Player, string) bool { return false }
		m.config.RankAtLeast = func(*player.Player, string) bool { return true }
		m.config.RankExists = func(r string) bool { return r == "vip" }
		m.Register()
		c, _ := cmd.ByAlias("warp")
		run := func(line string) { t.Helper(); c.Execute(line, p, tx) }
		get := func(name string) *Warp {
			t.Helper()
			w := m.get(name)
			if w == nil {
				t.Fatalf("no warp %s", name)
			}
			return w
		}
		dest := func(name string, pos mgl64.Vec3, rot cube.Rotation) {
			t.Helper()
			d := get(name).Destination
			if d == nil || d.Position != pos || d.Rotation != rot || d.World != "overworld" {
				t.Errorf("%s destination %+v, want %v %v", name, d, pos, rot)
			}
		}
		area := func(name string, lo, hi cube.Pos) {
			t.Helper()
			if w := get(name); w.Min != lo || w.Max != hi {
				t.Errorf("%s area %v %v, want %v %v", name, w.Min, w.Max, lo, hi)
			}
		}

		run("create pad")
		area("pad", cube.Pos{}, cube.Pos{})
		if get("pad").Destination != nil {
			t.Error("create set a destination")
		}
		run("create box 1 2 3 ~2 ~ ~")
		area("box", cube.Pos{1, 0, 0}, cube.Pos{2, 2, 3})
		run("create Box")
		if len(m.warps) != 2 {
			t.Error("duplicate create")
		}
		if !slices.Contains(warpName("").Options(p), "box") {
			t.Error("names enum misses box")
		}

		run("dest pad here")
		dest("pad", p.Position(), p.Rotation())
		run("dest pad 10.5 1 20.5 90 30")
		dest("pad", mgl64.Vec3{10.5, 1, 20.5}, cube.Rotation{90, 30})
		run("dest pad 11 2 21")
		dest("pad", mgl64.Vec3{11, 2, 21}, cube.Rotation{})
		run("dest pad ~1 ~ ~-1")
		dest("pad", p.Position().Add(mgl64.Vec3{1, 0, -1}), cube.Rotation{})
		run("dest pad 1 2 3 45")
		dest("pad", p.Position().Add(mgl64.Vec3{1, 0, -1}), cube.Rotation{}) // yaw alone: usage error

		run("set quick")
		area("quick", cube.Pos{}, cube.Pos{})
		dest("quick", p.Position(), p.Rotation())
		run("set quick 5 6 7")
		dest("quick", mgl64.Vec3{5, 6, 7}, cube.Rotation{})
		run("set quick 5 6 7 4 5")
		dest("quick", mgl64.Vec3{5, 6, 7}, cube.Rotation{4, 5})
		run("set quick here")
		dest("quick", p.Position(), p.Rotation())
		run("set fresh 1 0 1 2 0 2")
		area("fresh", cube.Pos{1, 0, 1}, cube.Pos{2, 0, 2})
		dest("fresh", p.Position(), p.Rotation())

		run("area pad 4 0 4 3 1 3")
		area("pad", cube.Pos{3, 0, 3}, cube.Pos{4, 1, 4})
		run("area pad 9 9 9")
		area("pad", cube.Pos{3, 0, 3}, cube.Pos{4, 1, 4}) // one corner only: rejected
		run("area pad")
		area("pad", cube.Pos{}, cube.Pos{})

		run("require pad pass")
		run("deny pad nope")
		run("rank pad vip")
		if w := get("pad"); w.Require != "pass" || w.Deny != "nope" || w.Rank != "vip" {
			t.Errorf("rules %+v", w)
		}
		run("rank pad ghost")
		run("require pad none")
		if w := get("pad"); w.Require != "" || w.Rank != "vip" {
			t.Errorf("rules after none/unknown rank %+v", w)
		}

		run("message pad &aHello   there world")
		if get("pad").Message != "&aHello there world" {
			t.Errorf("message %q", get("pad").Message)
		}
		run("message pad none")
		if get("pad").Message != "" {
			t.Error("message none")
		}
		run("fade pad on")
		if !get("pad").Fade {
			t.Error("fade on")
		}
		run("fade pad maybe")
		if !get("pad").Fade {
			t.Error("invalid fade changed the warp")
		}
		run("off pad")
		if get("pad").Enabled {
			t.Error("off")
		}
		run("on pad")
		if !get("pad").Enabled {
			t.Error("on")
		}
		run("rename quick fast")
		if m.get("quick") != nil || m.get("fast") == nil {
			t.Error("rename")
		}
		if !slices.Contains(warpName("").Options(p), "fast") {
			t.Error("names enum not refreshed after rename")
		}
		run("info pad")
		run("list")
		run("info ghost")
		run("bogus words")
		run("")

		run("edit pad")
		if ed := m.editing[p.UUID()]; ed == nil || ed.picked != "pad" {
			t.Error("edit pad")
		}
		run("edit")
		if m.editing[p.UUID()] != nil {
			t.Error("edit toggle off")
		}

		run("dest pad 30 4 30 10 0")
		run("tp pad")
		if p.Position() != (mgl64.Vec3{30, 4, 30}) {
			t.Errorf("tp went to %v", p.Position())
		}
		run("delete fast")
		run("remove fresh")
		run("DELETE box")
		if m.get("fast") != nil || m.get("fresh") != nil || m.get("box") != nil || m.get("pad") == nil {
			t.Error("delete/remove")
		}
	})
}

func TestOverloadsShownToClients(t *testing.T) {
	testWorld(t, func(m *Manager, _ *world.Tx, p *player.Player) {
		m.Register()
		c, _ := cmd.ByAlias("warp")
		params := c.Params(p)
		var shown []string
		for _, o := range params {
			var parts []string
			for _, pi := range o {
				switch v := pi.Value.(type) {
				case cmd.SubCommand:
					parts = append(parts, pi.Name)
				case cmd.Enum:
					parts = append(parts, v.Type())
				default:
					parts = append(parts, "<"+pi.Name+">")
				}
			}
			shown = append(shown, strings.Join(parts, " "))
		}
		for _, want := range []string{
			"WarpNameAction WarpName", "list", "edit WarpName", "create <name> <corner1> <corner2>",
			"area WarpName <corner1> <corner2>", "dest WarpName here", "dest WarpName <position> <yaw> <pitch>",
			"set <name>", "set <name> here", "set <name> <position> <yaw> <pitch>", "set <name> <corner1> <corner2>",
			"WarpRuleAction WarpName <value>", "message WarpName <text>", "fade WarpName WarpOnOff", "rename WarpName <new>", "",
		} {
			if !slices.Contains(shown, want) {
				t.Errorf("overload %q not shown; have %q", want, shown)
			}
		}
		if len(shown) != 16 {
			t.Errorf("%d overloads", len(shown))
		}
	})
}

func TestEditorGlassOverSolidBlocks(t *testing.T) {
	testWorld(t, func(m *Manager, tx *world.Tx, p *player.Player) {
		surface, centre, outside := cube.Pos{1, 0, 1}, cube.Pos{2, 1, 2}, cube.Pos{0, 0, 3}
		tx.SetBlock(surface, block.Stone{}, nil)
		tx.SetBlock(outside, block.Stone{}, nil)
		m.warps = []*Warp{{Name: "pad", World: "overworld", Min: cube.Pos{1, 0, 1}, Max: cube.Pos{3, 2, 3}, Enabled: true}}
		if err := m.run(p, "edit", []string{"pad"}, &cmd.Output{}); err != nil {
			t.Fatal(err)
		}
		ed := m.editing[p.UUID()]
		if _, ok := ed.shown[surface].(block.Glass); !ok {
			t.Error("solid area block not shown as glass")
		}
		if _, ok := ed.shown[centre]; ok {
			t.Error("enclosed air sent as glass")
		}
		if _, ok := ed.shown[outside]; ok {
			t.Error("block outside the area shown")
		}
		tx.SetBlock(centre, block.Stone{}, nil)
		m.drawLocked(p, ed, p.Position())
		if _, ok := ed.shown[centre].(block.Glass); !ok {
			t.Error("enclosed real block not hidden by glass")
		}

		// A client break sends its start and its end; neither may break the stone.
		p.StartBreaking(surface, cube.FaceUp)
		p.BreakBlock(surface)
		if _, ok := tx.Block(surface).(block.Stone); !ok {
			t.Fatal("editor broke the real block")
		}
		if m.get("pad").has(surface) {
			t.Error("area block not removed")
		}
		if _, ok := ed.shown[surface].(block.Stone); !ok {
			t.Errorf("real block not shown again: %T", ed.shown[surface])
		}
		p.BreakBlock(surface)
		if _, ok := tx.Block(surface).(block.Stone); !ok || len(m.get("pad").cells) != 26 {
			t.Error("guarded block broken or area changed")
		}
		// Ordinary blocks outside every area still break while editing.
		p.BreakBlock(outside)
		if _, ok := tx.Block(outside).(block.Air); !ok {
			t.Error("editor blocked an ordinary break")
		}
		// Placing glass against the real block adds to the area, without placing anything.
		p.SetHeldItems(item.NewStack(block.Glass{}, 4), item.Stack{})
		p.UseItemOnBlock(surface, cube.FaceWest, mgl64.Vec3{0, .5, .5})
		if !m.get("pad").has(cube.Pos{0, 0, 1}) {
			t.Error("glass placed against a real block did not add")
		}
		if _, ok := tx.Block(cube.Pos{0, 0, 1}).(block.Air); !ok {
			t.Error("real glass placed")
		}
		if err := m.run(p, "edit", nil, &cmd.Output{}); err != nil {
			t.Error(err)
		}
		p.BreakBlock(surface)
		if _, ok := tx.Block(surface).(block.Air); !ok {
			t.Error("real block still protected after the editor closed")
		}
	})
}
