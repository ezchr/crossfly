package javasession

import (
	"context"
	"fmt"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// TestJavaCrossbowHoldTraffic counts what a Java session is sent over two seconds of holding a
// sword, an empty crossbow, a crossbow being charged (and the release), and a loaded one. A Java
// client keeps charging until it lets go of the key: the loading-end sound was played every tick
// after the charge was complete, to everyone nearby; vanilla plays it once.
func TestJavaCrossbowHoldTraffic(t *testing.T) {
	run := func(name string, held item.Stack, use bool, release bool) {
		w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
		defer w.Close()
		s := mobTestSession()
		_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
			pos := mgl64.Vec3{0.5, 10, 0.5}
			h := world.EntitySpawnOpts{Position: pos}.New(player.Type, player.Config{
				Name: "A", UUID: uuid.New(), GameMode: world.GameModeSurvival, Health: 20, MaxHealth: 20, Position: pos, Session: s,
			})
			p := tx.AddEntity(h).(*player.Player)
			s.ent = p.H()
			l := world.NewLoader(0, tx.World(), holdViewer{s})
			l.Move(tx, pos)
			l.Load(tx, 1)
			clear(holdSounds)
			_ = p.Inventory().SetItem(0, held)
			_, _ = p.Inventory().AddItem(item.NewStack(item.Arrow{}, 8))
			p.SetHeldSlot(0)
			s.take()
			if use {
				p.UseItem()
				s.take()
			}
			counts := map[int32]int{}
			for i := 0; i < 40; i++ {
				time.Sleep(50 * time.Millisecond)
				p.Tick(tx, int64(i+1000))
				for _, o := range s.take() {
					counts[o.id]++
				}
			}
			if release {
				s.releaseUseItem(p)
				for i := 0; i < 20; i++ {
					p.Tick(tx, int64(i+2000))
					for _, o := range s.take() {
						counts[o.id+10000]++
					}
				}
			}
			keys := []int{}
			for k := range counts {
				keys = append(keys, int(k))
			}
			sort.Ints(keys)
			out := ""
			for _, k := range keys {
				out += fmt.Sprintf(" 0x%x:%d", k, counts[int32(k)])
			}
			t.Logf("%-18s 2s:%s using=%v sounds=%v", name, out, p.UsingItem(), holdSounds)
			ends := 0
			for k, v := range holdSounds {
				if strings.HasPrefix(k, "sound.CrossbowLoad{2 ") {
					ends += v
				}
			}
			if want := map[bool]int{true: 1, false: 0}[use && release]; ends != want {
				t.Errorf("%s: loading-end sound played %d times, want %d", name, ends, want)
			}
			if total := len(counts); !use && total != 0 {
				t.Errorf("%s: holding it sent packets %v", name, counts)
			}
			l.Close(tx)
			tx.RemoveEntity(p)
			return struct{}{}, nil
		})
	}
	run("sword", item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1), false, false)
	run("crossbow idle", item.NewStack(item.Crossbow{}, 1), false, false)
	run("crossbow charging", item.NewStack(item.Crossbow{}, 1), true, true)
	run("crossbow loaded", item.NewStack(item.Crossbow{Item: item.NewStack(item.Arrow{}, 1)}, 1), false, false)
	run("bow drawing", item.NewStack(item.Bow{}, 1), true, false)
}

type holdViewer struct{ *Session }

var holdSounds = map[string]int{}

func (n holdViewer) ViewSound(pos mgl64.Vec3, t world.Sound) {
	holdSounds[fmt.Sprintf("%T%v", t, t)]++
	n.Session.ViewSound(pos, t)
}

func (holdViewer) ViewChunk(world.ChunkPos, world.Dimension, map[cube.Pos]world.Block, *chunk.Chunk) {
}
