package warps

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

func testWorld(t *testing.T, fn func(*Manager, *world.Tx, *player.Player)) {
	t.Helper()
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	m, err := Open(Config{File: filepath.Join(t.TempDir(), "warps.json"), Worlds: map[string]*world.World{"overworld": w}, Admin: func(*player.Player) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = world.Call(ctx, w, func(tx *world.Tx) (struct{}, error) {
		pos := mgl64.Vec3{.5, 0, .5}
		h := world.EntitySpawnOpts{Position: pos}.New(player.Type, player.Config{Name: "Editor", UUID: uuid.New(), GameMode: world.GameModeCreative, Health: 20, MaxHealth: 20, Position: pos})
		p := tx.AddEntity(h).(*player.Player)
		p.Handle(m.Wrap(player.NopHandler{}))
		fn(m, tx, p)
		return struct{}{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestShapePersistenceAndNegativeCoordinates(t *testing.T) {
	testWorld(t, func(m *Manager, tx *world.Tx, p *player.Player) {
		o := &cmd.Output{}
		if err := m.run(p, "create", []string{"pad", "-2", "0", "-2", "0", "2", "0"}, o); err != nil {
			t.Error(err)
			return
		}
		if err := m.run(p, "dest", []string{"pad", "10.5", "1", "20.5", "90", "30"}, o); err != nil {
			t.Error(err)
		}
		if err := m.run(p, "edit", []string{"pad"}, o); err != nil {
			t.Error(err)
		}
		ctx := player.NewEventContext(tx, p)
		if !m.removeCell(ctx, cube.Pos{-1, 1, -1}) || !ctx.Cancelled() {
			t.Error("remove not intercepted")
		}
		w := m.get("pad")
		if w.has(cube.Pos{-1, 1, -1}) || len(w.cells) != 26 {
			t.Error("hole not retained")
		}
		loaded, err := Open(m.config)
		if err != nil {
			t.Error(err)
			return
		}
		if loaded.get("pad").has(cube.Pos{-1, 1, -1}) || !loaded.get("pad").has(cube.Pos{-2, 0, -2}) {
			t.Error("shape changed after restart")
		}
		if loaded.get("pad").Destination.Rotation != (cube.Rotation{90, 30}) {
			t.Error("rotation not saved")
		}
		pos, err := parsePosition([]string{"~-1", "~", "~-2"}, mgl64.Vec3{.5, 1, .5})
		if err != nil || cube.PosFromVec3(pos) != (cube.Pos{-1, 1, -2}) {
			t.Errorf("relative negative coords %v %v", pos, err)
		}
		for _, v := range []string{"NaN", "Inf", "30000001"} {
			if _, err := parsePosition([]string{v, "0", "0"}, mgl64.Vec3{}); err == nil {
				t.Errorf("accepted %s", v)
			}
		}
	})
}

func TestEditorOverlapLastCellAndNoRealPlacement(t *testing.T) {
	testWorld(t, func(m *Manager, tx *world.Tx, p *player.Player) {
		pos := cube.Pos{1, 1, 1}
		other := cube.Pos{2, 1, 1}
		m.warps = []*Warp{{Name: "a", World: "overworld", Min: pos, Max: other, Enabled: true}, {Name: "b", World: "overworld", Min: pos, Max: other, Enabled: true}}
		if err := m.run(p, "edit", []string{"a"}, &cmd.Output{}); err != nil {
			t.Error(err)
		}
		p.BreakBlock(pos)
		for _, w := range m.warps {
			if w.has(pos) || !w.has(other) {
				t.Errorf("overlap removal failed: %+v", w)
			}
		}
		p.BreakBlock(other)
		for _, w := range m.warps {
			if !w.has(other) {
				t.Error("deleted final cell")
			}
		}
		// Add against the remaining preview. No real glass is placed or consumed.
		p.SetHeldItems(item.NewStack(block.Glass{}, 8), item.Stack{})
		p.UseItemOnBlock(other, cube.FaceUp, mgl64.Vec3{.5, 1, .5})
		if !m.get("a").has(other.Side(cube.FaceUp)) {
			t.Error("could not extend preview")
		}
		if _, ok := tx.Block(other.Side(cube.FaceUp)).(block.Air); !ok {
			t.Error("editor placed real glass")
		}
		held, _ := p.HeldItems()
		if held.Count() != 8 {
			t.Error("editor consumed glass")
		}
		if err := m.run(p, "edit", nil, &cmd.Output{}); err != nil {
			t.Error(err)
		}
		if m.editing[p.UUID()] != nil {
			t.Error("edit toggle failed")
		}
	})
}

func TestWalkInWarpCooldownDisabledAndEditor(t *testing.T) {
	testWorld(t, func(m *Manager, tx *world.Tx, p *player.Player) {
		d := &Destination{World: "overworld", Position: mgl64.Vec3{10.5, 1, 10.5}, Rotation: cube.Rotation{45, 20}}
		w := &Warp{Name: "pad", World: "overworld", Min: cube.Pos{1, 0, 0}, Max: cube.Pos{1, 0, 0}, Destination: d, Enabled: false}
		m.warps = []*Warp{w}
		p.Move(mgl64.Vec3{1, 0, 0}, 0, 0)
		if p.Position() == d.Position {
			t.Error("disabled pad fired")
		}
		p.Teleport(mgl64.Vec3{.5, 0, .5})
		w.Enabled = true
		p.Move(mgl64.Vec3{1, 0, 0}, 0, 0)
		if p.Position() != d.Position || p.Rotation() != d.Rotation {
			t.Errorf("wrong destination/rotation: %v %v", p.Position(), p.Rotation())
		}
		p.Teleport(mgl64.Vec3{.5, 0, .5})
		p.Move(mgl64.Vec3{1, 0, 0}, 0, 0)
		if p.Position() == d.Position {
			t.Error("cooldown failed")
		}
		delete(m.cooldown, p.UUID())
		p.Teleport(mgl64.Vec3{.5, 0, .5})
		if err := m.run(p, "edit", []string{"pad"}, &cmd.Output{}); err != nil {
			t.Error(err)
		}
		p.Move(mgl64.Vec3{1, 0, 0}, 0, 0)
		if p.Position() == d.Position {
			t.Error("editor teleported")
		}
	})
}

func TestAccessRulesAndPendingCancellation(t *testing.T) {
	testWorld(t, func(m *Manager, tx *world.Tx, p *player.Player) {
		w := &Warp{Require: "pass", Rank: "vip"}
		m.config.Admin = func(*player.Player) bool { return false }
		if m.allowed(p, w) {
			t.Error("missing rank/tag passed")
		}
		m.config.HasTag = func(_ *player.Player, tag string) bool { return tag == "pass" }
		m.config.RankAtLeast = func(*player.Player, string) bool { return true }
		if !m.allowed(p, w) {
			t.Error("valid rank/tag denied")
		}
		w.Deny = "pass"
		if m.allowed(p, w) {
			t.Error("deny tag ignored")
		}
		m.config.Admin = func(*player.Player) bool { return true }
		if m.allowed(p, w) {
			t.Error("admin should skip deny-tag pad like Paper")
		}
		w.Deny = ""
		if !m.allowed(p, w) {
			t.Error("admin denied")
		}
		m.pending[p.UUID()] = 1
		p.Teleport(mgl64.Vec3{3, 0, 0})
		m.finishWarp(p, 1, Destination{World: "overworld", Position: mgl64.Vec3{50, 0, 0}}, "")
		if p.Position()[0] == 50 {
			t.Error("stale pending warp survived another teleport")
		}
	})
}

func TestInvalidFilePreservedAndVolumeBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "warps.json")
	raw := []byte(`{"warps":[null]}`)
	os.WriteFile(path, raw, 0600)
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	if _, err := Open(Config{File: path, Worlds: map[string]*world.World{"overworld": w}, Admin: func(*player.Player) bool { return true }}); err == nil {
		t.Error("accepted invalid saved data")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(raw) {
		t.Error("invalid file was overwritten")
	}
	area := Warp{Min: cube.Pos{-30000000, -2048, -30000000}, Max: cube.Pos{30000000, 2047, 30000000}}
	if area.shapeable() {
		t.Error("expanded enormous cuboid")
	}
	if area.volume() <= maxEditVolume {
		t.Error("volume overflow")
	}
	if finite(mgl64.Vec3{math.NaN(), 0, 0}) {
		t.Error("accepted nan")
	}
}

func TestCommandRegistration(t *testing.T) {
	testWorld(t, func(m *Manager, _ *world.Tx, p *player.Player) {
		m.Register()
		c, ok := cmd.ByAlias("warp")
		if !ok || len(c.Runnables(p)) == 0 {
			t.Error("warp command not registered")
		}
	})
}

func TestTeleportCommandAndStaleTicket(t *testing.T) {
	testWorld(t, func(m *Manager, tx *world.Tx, p *player.Player) {
		d := &Destination{World: "overworld", Position: mgl64.Vec3{20, 1, 20}, Rotation: cube.Rotation{90, 0}}
		m.warps = []*Warp{{Name: "pad", World: "overworld", Min: cube.Pos{}, Max: cube.Pos{}, Enabled: true, Destination: d}}
		if err := m.run(p, "tp", []string{"pad"}, &cmd.Output{}); err != nil {
			t.Error(err)
		}
		if p.Position() != d.Position {
			t.Error("tp command did not arrive")
		}
		m.pending[p.UUID()] = 2
		m.finishWarp(p, 1, *d, "")
		if m.pending[p.UUID()] != 2 {
			t.Error("stale ticket removed a newer pending warp")
		}
	})
}

func TestWorldTransferAndEditorRefresh(t *testing.T) {
	testWorld(t, func(m *Manager, tx *world.Tx, p *player.Player) {
		other := world.Config{Synchronous: true}.New()
		defer other.Close()
		m.config.Worlds["other"] = other
		m.warps = []*Warp{{Name: "pad", World: "overworld", Min: cube.Pos{1, 1, 1}, Max: cube.Pos{2, 1, 1}, Enabled: true}}
		pos := mgl64.Vec3{.5, 0, .5}
		h := world.EntitySpawnOpts{Position: pos}.New(player.Type, player.Config{Name: "SecondEditor", UUID: uuid.New(), GameMode: world.GameModeCreative, Health: 20, MaxHealth: 20, Position: pos})
		second := tx.AddEntity(h).(*player.Player)
		second.Handle(m.Wrap(player.NopHandler{}))
		for _, editor := range []*player.Player{p, second} {
			if err := m.run(editor, "edit", []string{"pad"}, &cmd.Output{}); err != nil {
				t.Error(err)
			}
		}
		if err := m.run(p, "delete", []string{"pad"}, &cmd.Output{}); err != nil {
			t.Error(err)
		}
		if m.editing[second.UUID()].revision != m.revision {
			t.Error("idle editor was not refreshed after deletion")
		}
		m.run(p, "edit", nil, &cmd.Output{})
		d := Destination{World: "other", Position: mgl64.Vec3{30, 5, 40}, Rotation: cube.Rotation{120, 15}}
		m.teleport(p, d)
		_, err := player.Call(context.Background(), h, func(_ *world.Tx, _ *player.Player) (bool, error) { return true, nil })
		if err != nil {
			t.Error(err)
		}
		_, err = player.Call(context.Background(), p.H(), func(dtx *world.Tx, np *player.Player) (bool, error) {
			if dtx.World() != other || np.Position() != d.Position || np.Rotation() != d.Rotation {
				t.Errorf("cross-world destination/rotation incorrect: %v %v", np.Position(), np.Rotation())
			}
			return true, nil
		})
		if err != nil {
			t.Error(err)
		}
	})
}
