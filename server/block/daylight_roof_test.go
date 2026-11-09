//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"testing"
)

func TestDaylightDetectorRespondsToRoofChanges(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	w.SetTime(6000)
	w.StopTime()
	at := cube.Pos{5, 64, 5}
	runWorld(w, func(tx *world.Tx) {
		tx.SetBlock(at, DaylightDetector{}, nil)
		(DaylightDetector{}).Tick(20, at, tx)
		if tx.Block(at).(DaylightDetector).Power != 15 {
			t.Fatal("open sky sensor did not reach full power")
		}
		for x := 4; x <= 6; x++ {
			for y := 63; y <= 65; y++ {
				for z := 4; z <= 6; z++ {
					p := cube.Pos{x, y, z}
					if p != at {
						tx.SetBlock(p, Stone{}, nil)
					}
				}
			}
		}
		tx.Block(at).(DaylightDetector).Tick(40, at, tx)
		if tx.Block(at).(DaylightDetector).Power != 0 {
			t.Fatal("newly sealed room kept stale skylight")
		}
		tx.SetBlock(at.Side(cube.FaceUp), Air{}, nil)
		tx.Block(at).(DaylightDetector).Tick(60, at, tx)
		if tx.Block(at).(DaylightDetector).Power != 15 {
			t.Fatal("removing the roof did not restore sunlight")
		}
	})
}
func TestInvertedDaylightDetectorCanBeSwitchedInNether(t *testing.T) {
	w := world.Config{Dim: world.Nether, Synchronous: true}.New()
	defer w.Close()
	at := cube.Pos{0, 64, 0}
	runWorld(w, func(tx *world.Tx) {
		tx.SetBlock(at, DaylightDetector{}, nil)
		(DaylightDetector{}).Activate(at, cube.FaceUp, tx, nil, &item.UseContext{})
		d := tx.Block(at).(DaylightDetector)
		if !d.Inverted || d.Power != 15 {
			t.Errorf("inverted no-sky detector: %+v", d)
		}
	})
}
