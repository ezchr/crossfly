package session

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// The world border for Bedrock players, who have no border of their own: pieces of wall that
// exist only for this player, each borderWallCell blocks long and fixed on a grid in the world,
// so the wall never moves. Clients draw no entity further than 72 blocks away on any axis, so a
// piece shows only near the player: borderWallRange. A piece shows only its columns inside the
// border, so that the sides end exactly at the corners.
//
// A square set with world.SetBorderRing is seen from afar instead. Its whole wall is in the model of
// one entity, once for each anchor of a fixed grid, at the wall's place in the world as seen from
// that anchor. The player gets the entity at the anchor nearest to them, close enough that clients
// always draw it whichever way they look, and moving to the next anchor shows the same wall in the
// same place.

const (
	// BorderCapEntity is the entity a piece of wall is drawn with.
	BorderCapEntity = "dragonfly:world_border_cap"
	// BorderRingEntity is the entity a whole square is drawn with.
	BorderRingEntity = "dragonfly:world_border"
)

const (
	borderWallCell = 32
	// borderWallRange is how far, on either axis, a piece is shown from the player.
	borderWallRange = 68
	// borderWallY is the middle of the wall, which reaches 192 blocks either way.
	borderWallY = 32
	// borderWallIDs are the runtime IDs of the pieces, far above any real entity's.
	borderWallIDs = uint64(1) << 40
	// borderScrollTicks is how long the texture takes to scroll a tile (see packbuilder).
	borderScrollTicks = 160
	// borderRingIDs are the runtime IDs of the ring, one for each place it can be.
	borderRingIDs = borderWallIDs + uint64(1)<<32
	// borderRingKeep is how far a player gets from their anchor on the ground before it changes:
	// past half of world.BorderRingCell, and short of the 25 blocks to the edge of the entity's box.
	borderRingKeep = 22
	// borderRingStep is how far up the ring moves with the player, a whole number of texture tiles
	// so that it looks the same, borderRingStepKeep how far they get from it upwards, and
	// borderRingLevels how many times it moves.
	borderRingStep     = 32
	borderRingStepKeep = 18
	borderRingLevels   = 10
)

// borderView is what a session has shown of the border, by runtime ID.
type borderView struct {
	walls map[uint64]borderWall
	// ring is where the ring is shown, if it is.
	ring borderRingAt
}

// borderRingAt is an anchor of the ring: its place on the grid and how far up it is.
type borderRingAt struct {
	shown       bool
	x, z, level int
	variant     int32
}

// borderWall is one piece as the client has it.
type borderWall struct {
	variant, columns int32
}

// ViewBorder shows the world border to a Bedrock player (world.BorderViewer).
func (s *Session) ViewBorder(b world.Border, pos mgl64.Vec3) {
	if !world.BorderWall() {
		return
	}
	want := map[uint64]borderPiece{}
	ring := s.viewBorderRing(b, pos)
	if b.Enabled() && !ring {
		minX, minZ, maxX, maxZ := b.Bounds()
		for side := range 4 {
			borderWallPieces(side, pos, minX, minZ, maxX, maxZ, want)
		}
	}
	if s.border.walls == nil {
		s.border.walls = map[uint64]borderWall{}
	}
	for id := range s.border.walls {
		if _, ok := want[id]; !ok {
			s.writePacket(&packet.RemoveActor{EntityUniqueID: int64(id)})
			delete(s.border.walls, id)
		}
	}
	variant := borderVariant(b)
	for id, piece := range want {
		s.showBorderWall(id, piece, variant)
	}
}

// viewBorderRing shows the ring if b is its square, and reports whether it is.
func (s *Session) viewBorderRing(b world.Border, pos mgl64.Vec3) bool {
	minX, minZ, maxX, maxZ, ok := world.BorderRing()
	if ok && b.Enabled() {
		x0, z0, x1, z1 := b.Bounds()
		ok = x0 == minX && z0 == minZ && x1 == maxX && z1 == maxZ && b.Remaining == 0
	} else {
		ok = false
	}
	old := s.border.ring
	if !ok {
		if old.shown {
			s.writePacket(&packet.RemoveActor{EntityUniqueID: int64(old.id())})
			s.border.ring = borderRingAt{}
		}
		return false
	}
	now := borderRingAt{
		shown:   true,
		x:       borderRingCell(minX, maxX, pos[0], old.x, old.shown),
		z:       borderRingCell(minZ, maxZ, pos[2], old.z, old.shown),
		level:   borderRingLevel(pos[1], old.level, old.shown),
		variant: borderVariant(b),
	}
	switch {
	case old.shown && old.id() == now.id() && old.variant == now.variant:
	case old.shown && old.id() == now.id():
		s.writePacket(&packet.SetActorData{EntityRuntimeID: old.id(), EntityMetadata: protocol.EntityMetadata{protocol.EntityDataKeyVariant: now.variant}})
	default:
		s.showBorderRing(now, mgl64.Vec3{
			world.BorderRingAnchor(minX, now.x),
			float64(now.level * borderRingStep),
			world.BorderRingAnchor(minZ, now.z),
		}, int32(now.x*world.BorderRingCells(minZ, maxZ)+now.z))
		if old.shown {
			s.writePacket(&packet.RemoveActor{EntityUniqueID: int64(old.id())})
		}
	}
	s.border.ring = now
	return true
}

// id is the runtime ID of the ring at this anchor.
func (r borderRingAt) id() uint64 {
	return borderRingIDs + uint64(r.x)<<16 + uint64(r.z)<<8 + uint64(r.level)
}

// borderRingCell is the anchor for a player at v, between from and to. A player stays with their
// anchor while clients still draw it, so that walking to and fro on a line doesn't swap it.
func borderRingCell(from, to, v float64, cur int, shown bool) int {
	if shown && math.Abs(v-world.BorderRingAnchor(from, cur)) <= borderRingKeep {
		return cur
	}
	return min(max(int(math.Floor((v-from)/world.BorderRingCell)), 0), world.BorderRingCells(from, to)-1)
}

// borderRingLevel is how far up the ring is for a player at height y.
func borderRingLevel(y float64, cur int, shown bool) int {
	if shown && math.Abs(y-float64(cur*borderRingStep)) <= borderRingStepKeep {
		return cur
	}
	return min(max(int(math.Round(y/borderRingStep)), 0), borderRingLevels-1)
}

// showBorderRing adds the ring at an anchor. part is the anchor's part of the model.
func (s *Session) showBorderRing(r borderRingAt, at mgl64.Vec3, part int32) {
	meta := protocol.NewEntityMetadata()
	meta.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagNoAI)
	meta[protocol.EntityDataKeyVariant] = r.variant
	meta[protocol.EntityDataKeyMarkVariant] = part
	meta[protocol.EntityDataKeySkinID] = int32(time.Now().UnixMilli() / 50 % borderScrollTicks)
	meta[protocol.EntityDataKeyWidth] = float32(0)
	meta[protocol.EntityDataKeyHeight] = float32(0)
	meta[protocol.EntityDataKeyScale] = float32(1)
	s.writePacket(&packet.AddActor{
		EntityUniqueID:  int64(r.id()),
		EntityRuntimeID: r.id(),
		EntityType:      BorderRingEntity,
		EntityMetadata:  meta,
		Position:        vec64To32(at),
	})
}

// borderPiece is a piece of wall: where it is, whether it is turned a quarter, and the columns it
// shows (first*128 + end, the render controller's part visibility).
type borderPiece struct {
	at      mgl64.Vec3
	turned  bool
	columns int32
}

// borderWallPieces adds the pieces of a side near a player at pos to want.
func borderWallPieces(side int, pos mgl64.Vec3, minX, minZ, maxX, maxZ float64, want map[uint64]borderPiece) {
	line, along, across, from, to := minZ, pos[0], pos[2], minX, maxX
	switch side {
	case 1:
		line = maxZ
	case 2:
		line, along, across, from, to = minX, pos[2], pos[0], minZ, maxZ
	case 3:
		line, along, across, from, to = maxX, pos[2], pos[0], minZ, maxZ
	}
	if math.Abs(across-line) > borderWallRange || math.Abs(pos[1]-borderWallY) > borderWallRange {
		return
	}
	cell := func(v float64) int { return int(math.Floor(v / borderWallCell)) }
	for c := cell(max(from, along-borderWallRange)); c <= cell(min(to, along+borderWallRange)); c++ {
		left := float64(c * borderWallCell)
		middle := left + borderWallCell/2
		if math.Abs(middle-along) > borderWallRange {
			continue
		}
		first := int32(min(max(math.Ceil(from-left), 0), borderWallCell))
		end := int32(min(max(math.Floor(to-left), 0), borderWallCell))
		if first >= end {
			continue
		}
		id := borderWallIDs + uint64(side)<<24 + uint64(c+1<<20)
		if side >= 2 {
			want[id] = borderPiece{at: mgl64.Vec3{line, borderWallY, middle}, turned: true, columns: first*128 + end}
			continue
		}
		want[id] = borderPiece{at: mgl64.Vec3{middle, borderWallY, line}, columns: first*128 + end}
	}
}

// borderVariant picks the wall's texture (the render controller's array, see packbuilder): 0-2
// Java's look while still, shrinking or growing, then the registered textures in order.
func borderVariant(b world.Border) int32 {
	if b.Texture != "" {
		for i, t := range world.BorderTextures() {
			if t.Name == b.Texture {
				return int32(3 + i)
			}
		}
	}
	switch {
	case b.Shrinking():
		return 1
	case b.Growing():
		return 2
	}
	return 0
}

// showBorderWall shows a piece, sending only changes.
func (s *Session) showBorderWall(id uint64, piece borderPiece, variant int32) {
	w, shown := s.border.walls[id]
	switch {
	case !shown:
		yaw := float32(0)
		if piece.turned {
			yaw = 90
		}
		meta := protocol.NewEntityMetadata()
		meta.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagNoAI)
		meta[protocol.EntityDataKeyVariant] = variant
		meta[protocol.EntityDataKeyMarkVariant] = piece.columns
		// Where the scroll is now, so that this piece runs with the others.
		meta[protocol.EntityDataKeySkinID] = int32(time.Now().UnixMilli() / 50 % borderScrollTicks)
		meta[protocol.EntityDataKeyWidth] = float32(0)
		meta[protocol.EntityDataKeyHeight] = float32(0)
		meta[protocol.EntityDataKeyScale] = float32(1)
		s.writePacket(&packet.AddActor{
			EntityUniqueID:  int64(id),
			EntityRuntimeID: id,
			EntityType:      BorderCapEntity,
			EntityMetadata:  meta,
			Position:        vec64To32(piece.at),
			Yaw:             yaw,
			HeadYaw:         yaw,
			BodyYaw:         yaw,
		})
	case variant != w.variant || piece.columns != w.columns:
		meta := protocol.EntityMetadata{protocol.EntityDataKeyVariant: variant, protocol.EntityDataKeyMarkVariant: piece.columns}
		s.writePacket(&packet.SetActorData{EntityRuntimeID: id, EntityMetadata: meta})
	}
	s.border.walls[id] = borderWall{variant: variant, columns: piece.columns}
}
