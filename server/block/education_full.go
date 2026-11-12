//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The allow, deny and border blocks of Bedrock Edition. BDS checks them for every player on every world: there is
// no Education Edition setting behind them (BlockSource::checkBlockPermissions, checkBlockDestroyPermissions,
// _getBlockPermissions). The column scan itself is world.Tx.BuildPermitted; the player calls it when placing and
// breaking blocks, for Bedrock and Java players alike.
func init() {
	world.EnableBuildMarkers()
	world.RegisterBlock(Allow{})
	world.RegisterBlock(Deny{})
	registerAll(allBorderBlocks())
	world.RegisterItem(Allow{})
	world.RegisterItem(Deny{})
	world.RegisterItem(BorderBlock{})
}

var hashAllow, hashDeny, hashBorderBlock = NextHash(), NextHash(), NextHash()

// Allow is the allow block: players may build in the column above it, up to the first deny block. It cannot be
// broken in survival and drops nothing (BDS VanillaBlockDrops dropNothing); the ender dragon and the wither do not
// destroy it.
type Allow struct {
	solid
}

// BuildAllowed ...
func (Allow) BuildAllowed() bool { return true }

// EncodeItem ...
func (Allow) EncodeItem() (string, int16) { return "minecraft:allow", 0 }

// EncodeBlock ...
func (Allow) EncodeBlock() (string, map[string]any) { return "minecraft:allow", nil }

// Hash ...
func (Allow) Hash() (uint64, uint64) { return hashAllow, 0 }

// Deny is the deny block: players may not build in the column above it, up to the first allow block, and cannot
// break the deny block itself. Only world builders can. It cannot be broken in survival and drops nothing.
type Deny struct {
	solid
}

// BuildAllowed ...
func (Deny) BuildAllowed() bool { return false }

// EncodeItem ...
func (Deny) EncodeItem() (string, int16) { return "minecraft:deny", 0 }

// EncodeBlock ...
func (Deny) EncodeBlock() (string, map[string]any) { return "minecraft:deny", nil }

// Hash ...
func (Deny) Hash() (uint64, uint64) { return hashDeny, 0 }

// BorderBlock is the border block: a wall (BDS BorderBlock derives from WallBlock) that turns its whole column, from
// the bottom of the world to the top, into an invisible wall nothing passes and no one builds in. It cannot be
// broken in survival.
type BorderBlock struct {
	transparent
	sourceWaterDisplacer

	// NorthConnection is the type of connection in the north direction of the post.
	NorthConnection WallConnectionType
	// EastConnection is the type of connection in the east direction of the post.
	EastConnection WallConnectionType
	// SouthConnection is the type of connection in the south direction of the post.
	SouthConnection WallConnectionType
	// WestConnection is the type of connection in the west direction of the post.
	WestConnection WallConnectionType
	// Post is if the wall is extended to the full height of a block or not.
	Post bool
}

// BorderColumn ...
func (BorderBlock) BorderColumn() {}

// wallBlock makes walls connect to border blocks.
func (BorderBlock) wallBlock() {}

func (b BorderBlock) wall() Wall {
	return Wall{NorthConnection: b.NorthConnection, EastConnection: b.EastConnection, SouthConnection: b.SouthConnection, WestConnection: b.WestConnection, Post: b.Post}
}

func borderFromWall(w Wall) BorderBlock {
	return BorderBlock{NorthConnection: w.NorthConnection, EastConnection: w.EastConnection, SouthConnection: w.SouthConnection, WestConnection: w.WestConnection, Post: w.Post}
}

// EncodeItem ...
func (BorderBlock) EncodeItem() (string, int16) { return "minecraft:border_block", 0 }

// EncodeBlock ...
func (b BorderBlock) EncodeBlock() (string, map[string]any) {
	return "minecraft:border_block", map[string]any{
		"wall_connection_type_north": b.NorthConnection.String(),
		"wall_connection_type_east":  b.EastConnection.String(),
		"wall_connection_type_south": b.SouthConnection.String(),
		"wall_connection_type_west":  b.WestConnection.String(),
		"wall_post_bit":              boolByte(b.Post),
	}
}

// Hash ...
func (b BorderBlock) Hash() (uint64, uint64) {
	return hashBorderBlock, uint64(b.NorthConnection.Uint8()) | uint64(b.EastConnection.Uint8())<<2 | uint64(b.SouthConnection.Uint8())<<4 | uint64(b.WestConnection.Uint8())<<6 | uint64(boolByte(b.Post))<<8
}

// Model ...
func (b BorderBlock) Model() world.BlockModel {
	return model.Wall{
		NorthConnection: b.NorthConnection.Height(),
		EastConnection:  b.EastConnection.Height(),
		SouthConnection: b.SouthConnection.Height(),
		WestConnection:  b.WestConnection.Height(),
		Post:            b.Post,
	}
}

// NeighbourUpdateTick ...
func (b BorderBlock) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	w, connectionsUpdated := b.wall().calculateConnections(tx, pos)
	w, postUpdated := w.calculatePost(tx, pos)
	if connectionsUpdated || postUpdated {
		tx.SetBlock(pos, borderFromWall(w), nil)
	}
}

// UseOnBlock ...
func (b BorderBlock) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, _, used = firstReplaceable(tx, pos, face, b)
	if !used {
		return
	}
	w, _ := b.wall().calculateConnections(tx, pos)
	w, _ = w.calculatePost(tx, pos)
	place(tx, pos, borderFromWall(w), user, ctx)
	return placed(ctx)
}

// SideClosed ...
func (BorderBlock) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool { return false }

// allBorderBlocks returns every state of the border block.
func allBorderBlocks() (b []world.Block) {
	for _, n := range WallConnectionTypes() {
		for _, e := range WallConnectionTypes() {
			for _, s := range WallConnectionTypes() {
				for _, w := range WallConnectionTypes() {
					for _, post := range []bool{false, true} {
						b = append(b, BorderBlock{NorthConnection: n, EastConnection: e, SouthConnection: s, WestConnection: w, Post: post})
					}
				}
			}
		}
	}
	return
}
