//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"testing"
)

func TestPistonResolverLimit(t *testing.T) {
	for _, count := range []int{0, 1, 12, 13} {
		w := world.Config{Dim: world.End, Synchronous: true}.New()
		pos := cube.Pos{0, 64, 0}
		runWorld(w, func(tx *world.Tx) {
			for i := 1; i <= count; i++ {
				tx.SetBlock(cube.Pos{i, 64, 0}, Stone{}, nil)
			}
			r, ok := resolvePiston(tx, pos, cube.FaceEast, true)
			if ok != (count <= 12) {
				t.Errorf("%d blocks: success %t", count, ok)
			}
			if ok && len(r.push) != count {
				t.Errorf("%d blocks: resolved %d", count, len(r.push))
			}
		})
		w.Close()
	}
}
func TestPistonResolverStickyBranches(t *testing.T) {
	w := world.Config{Dim: world.End, Synchronous: true}.New()
	defer w.Close()
	runWorld(w, func(tx *world.Tx) {
		honey, ok := world.BlockByName("minecraft:honey_block", nil)
		if !ok {
			t.Fatal("honey missing")
		}
		pos := cube.Pos{0, 64, 0}
		tx.SetBlock(pos.Side(cube.FaceEast), Slime{}, nil)
		tx.SetBlock(cube.Pos{1, 65, 0}, Stone{}, nil)
		tx.SetBlock(cube.Pos{1, 64, 1}, honey, nil)
		tx.SetBlock(cube.Pos{1, 64, -1}, Obsidian{}, nil)
		r, ok := resolvePiston(tx, pos, cube.FaceEast, true)
		if !ok || len(r.push) != 2 {
			t.Fatalf("slime should carry stone, ignore honey/obsidian: %v %v", ok, r.push)
		}
		tx.SetBlock(cube.Pos{2, 65, 0}, Obsidian{}, nil)
		if _, ok := resolvePiston(tx, pos, cube.FaceEast, true); ok {
			t.Error("blocked side branch was movable")
		}
	})
}
func TestPistonRejectsContainersAndPulls(t *testing.T) {
	w := world.Config{Dim: world.End, Synchronous: true}.New()
	defer w.Close()
	pos := cube.Pos{0, 64, 0}
	runWorld(w, func(tx *world.Tx) {
		tx.SetBlock(pos.Side(cube.FaceEast), NewDispenser(), nil)
		if _, ok := resolvePiston(tx, pos, cube.FaceEast, true); ok {
			t.Error("Java piston pushed a block entity")
		}
		tx.SetBlock(pos.Side(cube.FaceEast), Air{}, nil)
		tx.SetBlock(cube.Pos{2, 64, 0}, Stone{}, nil)
		r, ok := resolvePiston(tx, pos, cube.FaceEast, false)
		if !ok || len(r.push) != 1 || r.direction != cube.FaceWest {
			t.Errorf("sticky pull: success %t, blocks %v", ok, r.push)
		}
	})
}
func TestPistonPushPullLifecycle(t *testing.T) {
	for _, sticky := range []bool{false, true} {
		w := world.Config{Dim: world.End, Synchronous: true}.New()
		pos := cube.Pos{0, 64, 0}
		runWorld(w, func(tx *world.Tx) {
			tx.SetBlock(cube.Pos{1, 64, 0}, Stone{}, nil)
			tx.SetBlock(pos, Piston{Facing: cube.FaceEast, Sticky: sticky}, nil)
			tx.SetBlock(pos.Side(cube.FaceWest), RedstoneBlock{}, nil)
		})
		for range 4 {
			w.AdvanceTick()
		}
		runWorld(w, func(tx *world.Tx) {
			p := tx.Block(pos).(Piston)
			if !p.Extended {
				t.Error("piston did not extend")
			}
			if _, ok := tx.Block(cube.Pos{2, 64, 0}).(Stone); !ok {
				t.Errorf("stone not pushed: %T", tx.Block(cube.Pos{2, 64, 0}))
			}
			tx.SetBlock(pos.Side(cube.FaceWest), Air{}, nil)
		})
		for range 4 {
			w.AdvanceTick()
		}
		runWorld(w, func(tx *world.Tx) {
			if tx.Block(pos).(Piston).Extended {
				t.Error("piston did not retract")
			}
			target := cube.Pos{2, 64, 0}
			if sticky {
				target = cube.Pos{1, 64, 0}
			}
			if _, ok := tx.Block(target).(Stone); !ok {
				t.Errorf("sticky=%t: wrong final block at %v: %T", sticky, target, tx.Block(target))
			}
		})
		w.Close()
	}
}
