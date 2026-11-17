package javasession

import (
	"context"
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// assistBlocks is a world.BlockSource of a few blocks in the air.
type assistBlocks map[cube.Pos]world.Block

func (m assistBlocks) Block(pos cube.Pos) world.Block {
	if b, ok := m[pos]; ok {
		return b
	}
	return block.Air{}
}

const standEye, sneakEye = 1.62, 1.27

// TestBridgeAssistTarget: the block and face the placement help of Bedrock Edition picks.
func TestBridgeAssistTarget(t *testing.T) {
	stone := block.Stone{}
	slab := block.Slab{Block: block.Stone{}}
	topSlab := block.Slab{Block: block.Stone{}, Top: true}
	type want struct {
		ok     bool
		pos    cube.Pos
		face   cube.Face
		clickY float64
	}
	no := want{}
	onStone := assistBlocks{{0, 9, 0}: stone}
	centre := mgl64.Vec3{0.5, 10, 0.5}
	tests := []struct {
		name       string
		blocks     assistBlocks
		feet       mgl64.Vec3
		eye        float64
		yaw, pitch float64
		want       want
	}{
		{"standing on a block, looking south and down", onStone, centre, standEye, 0, 50,
			want{true, cube.Pos{0, 9, 0}, cube.FaceSouth, 1}},
		{"looking west", onStone, centre, standEye, 90, 50,
			want{true, cube.Pos{0, 9, 0}, cube.FaceWest, 1}},
		{"looking north", onStone, centre, standEye, 180, 50,
			want{true, cube.Pos{0, 9, 0}, cube.FaceNorth, 1}},
		{"looking east, yaw below zero", onStone, centre, standEye, -90, 50,
			want{true, cube.Pos{0, 9, 0}, cube.FaceEast, 1}},
		{"yaw past a full turn", onStone, centre, standEye, 360 + 170, 50,
			want{true, cube.Pos{0, 9, 0}, cube.FaceNorth, 1}},
		{"between south and west: the larger part wins", onStone, centre, standEye, 40, 50,
			want{true, cube.Pos{0, 9, 0}, cube.FaceSouth, 1}},
		{"between south and west, past the diagonal", onStone, centre, standEye, 50, 50,
			want{true, cube.Pos{0, 9, 0}, cube.FaceWest, 1}},
		{"looking straight down: the face follows the yaw", onStone, centre, standEye, 90, 90,
			want{true, cube.Pos{0, 9, 0}, cube.FaceWest, 1}},
		{"just steep enough (44.5 degrees)", onStone, centre, standEye, 0, 44.5,
			want{true, cube.Pos{0, 9, 0}, cube.FaceSouth, 1}},
		{"not steep enough (44 degrees)", onStone, centre, standEye, 0, 44, no},
		{"looking ahead", onStone, centre, standEye, 0, 0, no},
		{"looking up", onStone, centre, standEye, 0, -80, no},
		{"on a bottom slab", assistBlocks{{0, 9, 0}: slab}, mgl64.Vec3{0.5, 9.5, 0.5}, standEye, 0, 60,
			want{true, cube.Pos{0, 9, 0}, cube.FaceSouth, 0.5}},
		{"on a top slab", assistBlocks{{0, 9, 0}: topSlab}, centre, standEye, 0, 60,
			want{true, cube.Pos{0, 9, 0}, cube.FaceSouth, 1}},
		{"a slab on the block: the slab is the target", assistBlocks{{0, 9, 0}: stone, {0, 10, 0}: slab}, mgl64.Vec3{0.5, 10.5, 0.5}, standEye, 0, 60,
			want{true, cube.Pos{0, 10, 0}, cube.FaceSouth, 0.5}},
		{"a plant at the feet is the target (no collision box)", assistBlocks{{0, 9, 0}: stone, {0, 10, 0}: block.ShortGrass{}}, centre, standEye, 0, 60,
			want{true, cube.Pos{0, 10, 0}, cube.FaceSouth, 1}},
		{"water at the feet is looked through", assistBlocks{{0, 9, 0}: stone, {0, 10, 0}: block.Water{Depth: 8, Still: true}}, centre, standEye, 0, 60,
			want{true, cube.Pos{0, 9, 0}, cube.FaceSouth, 1}},
		{"east edge of a chunk", assistBlocks{{15, 9, 0}: stone, {16, 9, 0}: block.Dirt{}}, mgl64.Vec3{15.9, 10, 0.5}, standEye, -90, 60,
			want{true, cube.Pos{15, 9, 0}, cube.FaceEast, 1}},
		{"just past the chunk edge: the next block", assistBlocks{{15, 9, 0}: stone, {16, 9, 0}: block.Dirt{}}, mgl64.Vec3{16.1, 10, 0.5}, standEye, -90, 60,
			want{true, cube.Pos{16, 9, 0}, cube.FaceEast, 1}},
		{"below zero: the block at -1", assistBlocks{{-1, 9, -16}: stone}, mgl64.Vec3{-0.3, 10, -15.2}, standEye, 180, 60,
			want{true, cube.Pos{-1, 9, -16}, cube.FaceNorth, 1}},
		{"below y 0", assistBlocks{{0, -61, 0}: stone}, mgl64.Vec3{0.5, -60, 0.5}, standEye, 0, 60,
			want{true, cube.Pos{0, -61, 0}, cube.FaceSouth, 1}},
		{"sneaking over the edge, centre on the block", onStone, mgl64.Vec3{0.5, 10, 0.95}, sneakEye, 0, 70,
			want{true, cube.Pos{0, 9, 0}, cube.FaceSouth, 1}},
		{"sneaking over the edge, centre past it", onStone, mgl64.Vec3{0.5, 10, 1.25}, sneakEye, 0, 70, no},
		{"centre over the next block: that one", assistBlocks{{0, 9, 0}: stone, {0, 9, 1}: block.Dirt{}}, mgl64.Vec3{0.5, 10, 1.25}, standEye, 0, 70,
			want{true, cube.Pos{0, 9, 1}, cube.FaceSouth, 1}},
		{"a block already in front: same target", assistBlocks{{0, 9, 0}: stone, {0, 9, 1}: stone}, centre, standEye, 0, 50,
			want{true, cube.Pos{0, 9, 0}, cube.FaceSouth, 1}},
		{"in a jump, 0.3 above the block", onStone, mgl64.Vec3{0.5, 10.3, 0.5}, standEye, 0, 60,
			want{true, cube.Pos{0, 9, 0}, cube.FaceSouth, 1}},
		{"out of reach: 0.5 above the block", onStone, mgl64.Vec3{0.5, 10.5, 0.5}, standEye, 0, 60, no},
		{"out of reach: falling, the ground far below", assistBlocks{{0, 2, 0}: stone}, centre, standEye, 0, 60, no},
		{"nothing below", assistBlocks{}, centre, standEye, 0, 60, no},
		{"a fence post the centre misses", assistBlocks{{0, 9, 0}: block.WoodFence{}}, mgl64.Vec3{0.1, 10.5, 0.1}, standEye, 0, 60, no},
		{"NaN position", onStone, mgl64.Vec3{math.NaN(), 10, 0.5}, standEye, 0, 60, no},
		{"NaN pitch", onStone, centre, standEye, 0, math.NaN(), no},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eye := tt.feet.Add(mgl64.Vec3{0, tt.eye})
			pos, face, click, ok := bridgeAssistTarget(tt.blocks, eye, cube.Rotation{tt.yaw, tt.pitch})
			if ok != tt.want.ok {
				t.Fatalf("ok %v, want %v (pos %v face %v)", ok, tt.want.ok, pos, face)
			}
			if !ok {
				return
			}
			if pos != tt.want.pos || face != tt.want.face {
				t.Fatalf("target %v %v, want %v %v", pos, face, tt.want.pos, tt.want.face)
			}
			if math.Abs(click[1]-tt.want.clickY) > 1e-9 {
				t.Fatalf("click y %v, want %v", click[1], tt.want.clickY)
			}
			for i, v := range click {
				if v < 0 || v > 1 {
					t.Fatalf("click[%d] = %v is outside the block", i, v)
				}
			}
		})
	}
}

// TestBridgeAssistEnv: CF_JAVA_BRIDGE_ASSIST overrides the setting only when it is set to on or off.
func TestBridgeAssistEnv(t *testing.T) {
	for _, tt := range []struct {
		env        string
		conf, want bool
	}{{"", false, false}, {"", true, true}, {"1", false, true}, {"on", false, true}, {"0", true, false}, {"OFF", true, false}, {"maybe", true, true}} {
		t.Setenv("CF_JAVA_BRIDGE_ASSIST", tt.env)
		if got := bridgeAssistEnv(tt.conf); got != tt.want {
			t.Errorf("env %q, setting %v: %v, want %v", tt.env, tt.conf, got, tt.want)
		}
	}
}

// assistWorld runs f with a survival player standing on a stone block at 0,9,0 in an empty world, holding
// 8 stone.
func assistWorld(t *testing.T, f func(tx *world.Tx, s *Session, p *player.Player)) {
	t.Helper()
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		tx.SetBlock(cube.Pos{0, 9, 0}, block.Stone{}, nil)
		pos := mgl64.Vec3{0.5, 10, 0.5}
		h := world.EntitySpawnOpts{Position: pos}.New(player.Type, player.Config{
			Name: "Bridger", UUID: uuid.New(), GameMode: world.GameModeSurvival, Health: 20, MaxHealth: 20, Position: pos,
		})
		p := tx.AddEntity(h).(*player.Player)
		_ = p.Inventory().SetItem(0, item.NewStack(block.Stone{}, 8))
		_ = p.SetHeldSlot(0)
		s := mobTestSession()
		s.ent = p.H()
		s.bridgeAssist = true
		// The session sees its player, as the session of a player that joined does.
		l := world.NewLoader(1, w, selfActions{s: s})
		l.Move(tx, pos)
		l.Load(tx, 1)
		f(tx, s, p)
		return struct{}{}, nil
	})
}

func heldCount(p *player.Player) int {
	held, _ := p.HeldItems()
	return held.Count()
}

func isAir(tx *world.Tx, pos cube.Pos) bool {
	_, ok := tx.Block(pos).(block.Air)
	return ok
}

// TestBridgeAssistPlaces: a use_item while looking down at nothing places the held block in front of the
// block the player stands on, takes one from the stack and swings the arm on the client.
func TestBridgeAssistPlaces(t *testing.T) {
	assistWorld(t, func(tx *world.Tx, s *Session, p *player.Player) {
		if !s.bridgeAssistUse(tx, p, 0, 50) {
			t.Fatal("no assisted placement")
		}
		if _, ok := tx.Block(cube.Pos{0, 9, 1}).(block.Stone); !ok {
			t.Fatalf("block south of the support block: %T, want stone", tx.Block(cube.Pos{0, 9, 1}))
		}
		if n := heldCount(p); n != 7 {
			t.Fatalf("held count %d, want 7", n)
		}
		swung := false
		for _, o := range s.take() {
			if o.id == v777.ClientboundPlaySwingAnimation {
				swung = true
			}
		}
		if !swung {
			t.Fatal("the client was not told to swing its arm")
		}

		// The same click again: the place is taken, nothing happens and the use_item goes on as usual.
		if s.bridgeAssistUse(tx, p, 0, 50) {
			t.Fatal("assisted placement into a block that is already there")
		}
		if n := heldCount(p); n != 7 {
			t.Fatalf("held count %d after a click with no room, want 7", n)
		}
		// Another side is still free.
		if !s.bridgeAssistUse(tx, p, 90, 50) || isAir(tx, cube.Pos{-1, 9, 0}) {
			t.Fatal("no assisted placement on the west side")
		}
	})
}

// TestBridgeAssistNotUsed: the cases the assist leaves alone or that the placement rules refuse.
func TestBridgeAssistNotUsed(t *testing.T) {
	south := cube.Pos{0, 9, 1}
	t.Run("looking ahead", func(t *testing.T) {
		assistWorld(t, func(tx *world.Tx, s *Session, p *player.Player) {
			if s.bridgeAssistUse(tx, p, 0, 20) || !isAir(tx, south) {
				t.Fatal("placed while looking ahead")
			}
		})
	})
	t.Run("not holding a block", func(t *testing.T) {
		assistWorld(t, func(tx *world.Tx, s *Session, p *player.Player) {
			_ = p.Inventory().SetItem(0, item.NewStack(item.Stick{}, 8))
			if s.bridgeAssistUse(tx, p, 0, 50) || !isAir(tx, south) {
				t.Fatal("assisted a stick")
			}
			_ = p.Inventory().SetItem(0, item.Stack{})
			if s.bridgeAssistUse(tx, p, 0, 50) {
				t.Fatal("assisted an empty hand")
			}
		})
	})
	t.Run("adventure mode", func(t *testing.T) {
		assistWorld(t, func(tx *world.Tx, s *Session, p *player.Player) {
			p.SetGameMode(world.GameModeAdventure)
			if s.bridgeAssistUse(tx, p, 0, 50) || !isAir(tx, south) || heldCount(p) != 8 {
				t.Fatal("placed in adventure mode")
			}
		})
	})
	t.Run("an entity in the way", func(t *testing.T) {
		assistWorld(t, func(tx *world.Tx, s *Session, p *player.Player) {
			at := mgl64.Vec3{0.5, 9, 1.5}
			other := world.EntitySpawnOpts{Position: at}.New(player.Type, player.Config{
				Name: "Other", UUID: uuid.New(), GameMode: world.GameModeSurvival, Health: 20, MaxHealth: 20, Position: at,
			})
			tx.AddEntity(other)
			if s.bridgeAssistUse(tx, p, 0, 50) || !isAir(tx, south) || heldCount(p) != 8 {
				t.Fatal("placed into another player")
			}
		})
	})
	t.Run("a handler that cancels the placement", func(t *testing.T) {
		assistWorld(t, func(tx *world.Tx, s *Session, p *player.Player) {
			h := &denyPlace{}
			p.Handle(h)
			if s.bridgeAssistUse(tx, p, 0, 50) || !isAir(tx, south) || heldCount(p) != 8 {
				t.Fatal("placed although the handler cancelled it")
			}
			if h.useOn != 1 || h.place != 1 {
				t.Fatalf("handler calls: use on block %d, place %d, want 1 and 1", h.useOn, h.place)
			}
			if h.pos != south {
				t.Fatalf("handler asked about %v, want %v", h.pos, south)
			}
		})
	})
	t.Run("creative keeps the stack", func(t *testing.T) {
		assistWorld(t, func(tx *world.Tx, s *Session, p *player.Player) {
			p.SetGameMode(world.GameModeCreative)
			if !s.bridgeAssistUse(tx, p, 0, 50) || isAir(tx, south) || heldCount(p) != 8 {
				t.Fatal("creative placement: not placed, or the stack shrank")
			}
		})
	})
}

// TestBridgeAssistSlab: a slab used with the assist lands as a top slab, level with the block the player
// stands on (the click is on the top of that block).
func TestBridgeAssistSlab(t *testing.T) {
	assistWorld(t, func(tx *world.Tx, s *Session, p *player.Player) {
		_ = p.Inventory().SetItem(0, item.NewStack(block.Slab{Block: block.Stone{}}, 8))
		if !s.bridgeAssistUse(tx, p, 0, 50) {
			t.Fatal("no assisted placement")
		}
		sl, ok := tx.Block(cube.Pos{0, 9, 1}).(block.Slab)
		if !ok || !sl.Top {
			t.Fatalf("placed %#v, want a top slab", tx.Block(cube.Pos{0, 9, 1}))
		}
	})
}

// denyPlace cancels every block placement and counts what it was asked.
type denyPlace struct {
	player.NopHandler
	useOn, place int
	pos          cube.Pos
}

func (h *denyPlace) HandleItemUseOnBlock(*player.Context, cube.Pos, cube.Face, mgl64.Vec3) { h.useOn++ }
func (h *denyPlace) HandleBlockPlace(ctx *player.Context, pos cube.Pos, _ world.Block) {
	h.place++
	h.pos = pos
	ctx.Cancel()
}

// selfActions passes the entity actions a viewer of the world gets on to a session.
type selfActions struct {
	world.NopViewer
	s *Session
}

func (v selfActions) ViewEntityAction(e world.Entity, a world.EntityAction) {
	v.s.ViewEntityAction(e, a)
}
