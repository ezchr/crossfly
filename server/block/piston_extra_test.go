//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
	"testing"
)

func TestPistonBedrockSaveAndMovingRoundTrip(t *testing.T) {
	p := Piston{Facing: cube.FaceEast, Sticky: true, Extended: true, State: 1, Progress: .5, Started: 123, Attached: []cube.Pos{{2, 64, 0}}}
	raw, err := nbt.Marshal(p.EncodeNBT())
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := nbt.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	got := (Piston{Facing: cube.FaceEast, Sticky: true}).DecodeNBT(saved).(Piston)
	if !got.Extended || got.State != 1 || got.Progress != .5 || got.Started != 123 || len(got.Attached) != 1 {
		t.Fatalf("bad piston save: %+v", got)
	}
	m := MovingPiston{Moving: Stone{}, Piston: cube.Pos{1, 64, 0}, Facing: cube.FaceEast, Extending: true, Progress: .5, LastProgress: 0, Started: 123}
	raw, err = nbt.Marshal(m.EncodeNBT())
	if err != nil {
		t.Fatal(err)
	}
	if err = nbt.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	moved := m.DecodeNBT(saved).(MovingPiston)
	if _, ok := moved.Moving.(Stone); !ok || moved.Piston != m.Piston || moved.Facing != cube.FaceEast || moved.Progress != .5 {
		t.Fatalf("bad moving save: %+v", moved)
	}
}

func TestPistonHeadRemovalDestroysMatchingBase(t *testing.T) {
	w := world.Config{Dim: world.End, Synchronous: true, Entities: redstoneBreakDropTestEntityRegistry()}.New()
	defer w.Close()
	pos := cube.Pos{0, 64, 0}
	runWorld(w, func(tx *world.Tx) {
		p := Piston{Facing: cube.FaceEast, Extended: true, State: 2}
		tx.SetBlock(pos, p, nil)
		tx.SetBlock(pos.Side(cube.FaceEast), PistonHead{Facing: cube.FaceEast}, nil)
		tx.SetBlock(pos.Side(cube.FaceEast), nil, nil)
		if _, ok := tx.Block(pos).(Air); !ok {
			t.Error("removing piston head did not remove the base")
		}
	})
}
