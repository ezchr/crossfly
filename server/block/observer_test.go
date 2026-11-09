//go:build crossfly_full

package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestObserverStateUpdates(t *testing.T) {
	for _, face := range cube.Faces() {
		t.Run(face.String(), func(t *testing.T) {
			w := world.Config{Dim: world.End, Synchronous: true}.New()
			defer w.Close()
			pos := cube.Pos{0, 64, 0}
			o := Observer{Facing: face}
			runWorld(w, func(tx *world.Tx) {
				// An unviewed world does not advance its clock, making pending ticks deterministic.
				tx.SetBlock(pos, o, nil)
				tx.CancelBlockUpdate(pos, o) // Ignore the initial placement pulse when checking subsequent updates.
				tx.NotifyNeighbour(pos, pos.Side(face))
				if tx.BlockUpdateScheduled(pos, o) {
					t.Error("ordinary neighbour notification triggered observer")
				}
				tx.SetBlock(pos.Side(face.Opposite()), Stone{}, nil)
				if tx.BlockUpdateScheduled(pos, o) {
					t.Error("output-face change triggered observer")
				}
				tx.SetBlock(pos.Side(face), Air{}, nil)
				if tx.BlockUpdateScheduled(pos, o) {
					t.Error("identical state write triggered observer")
				}
				tx.SetBlock(pos.Side(face), Stone{}, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true})
				if !tx.BlockUpdateScheduled(pos, o) {
					t.Error("state write suppressing ordinary notifications did not trigger observer")
				}
			})
		})
	}
}

func TestObserverPoweredPlacementAndPulse(t *testing.T) {
	w := world.Config{Dim: world.End, Synchronous: true}.New()
	defer w.Close()
	pos := cube.Pos{0, 64, 0}
	o := Observer{Facing: cube.FaceEast, Powered: true}
	runWorld(w, func(tx *world.Tx) {
		tx.SetBlock(pos, o, nil)
		placed := tx.Block(pos).(Observer)
		if placed.Powered {
			t.Fatal("powered observer placement did not clear power")
		}
		placed.ScheduledTick(pos, tx, nil)
		on := tx.Block(pos).(Observer)
		if !on.Powered || !tx.BlockUpdateScheduled(pos, on) {
			t.Fatal("observer pulse did not turn on and schedule its end")
		}
		for _, face := range cube.Faces() {
			want := 0
			if face == cube.FaceWest {
				want = 15
			}
			if got := on.RedstonePower(pos, tx, face); got != want {
				t.Errorf("weak output at %s: %d, want %d", face, got, want)
			}
			if got := on.RedstoneStrongPower(pos, tx, face); got != want {
				t.Errorf("strong output at %s: %d, want %d", face, got, want)
			}
		}
		on.ScheduledTick(pos, tx, nil)
		if tx.Block(pos).(Observer).Powered {
			t.Error("pulse did not end")
		}
	})
}
