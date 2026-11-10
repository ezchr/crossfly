//go:build anchorwip

package block

// WIP (not compiled yet): needs ExplosionConfig.NoBlockDamage, sound.RespawnAnchor*, chat.MessageRespawnAnchorSet,
// registration, hash, redstone_analog case. See HANDOFF.md.

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/gameevent"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/google/uuid"
)

// RespawnAnchorMaxCharge is the most charges a respawn anchor holds.
const RespawnAnchorMaxCharge = 4

// RespawnAnchor sets a player's spawn point in the Nether (BDS 1.21.3 RespawnAnchorBlock): glowstone charges it
// (up to 4), using it with a charge in the Nether sets the spawn point, anywhere else it is removed and a fiery
// explosion of size 5 starts at its centre. Light level and comparator reading are floor(charge/4 * 15).
type RespawnAnchor struct {
	solid
	bassDrum

	// Charge is the number of charges of the anchor, from 0 to RespawnAnchorMaxCharge.
	Charge int
}

// LightEmissionLevel is floor(charge/4 * 15) (RespawnAnchorBlock::getLightEmission).
func (r RespawnAnchor) LightEmissionLevel() uint8 { return uint8(respawnAnchorScaled(r.Charge)) }

// ComparatorSignal is floor(charge/4 * 15) (RespawnAnchorBlock::getComparatorSignal; Java is the same).
func (r RespawnAnchor) ComparatorSignal() int { return respawnAnchorScaled(r.Charge) }

func respawnAnchorScaled(charge int) int {
	charge = min(max(charge, 0), RespawnAnchorMaxCharge)
	return int(float32(charge) / float32(RespawnAnchorMaxCharge) * 15)
}

// BreakInfo ...
func (r RespawnAnchor) BreakInfo() BreakInfo {
	return newBreakInfo(50, func(t item.Tool) bool {
		return t.ToolType() == item.TypePickaxe && t.HarvestLevel() >= item.ToolTierDiamond.HarvestLevel
	}, pickaxeEffective, oneOf(RespawnAnchor{})).withBlastResistance(1200)
}

type spawnPointUser interface {
	UUID() uuid.UUID
	Messaget(t chat.Translation, a ...any)
}

// Activate is RespawnAnchorBlock::use.
func (r RespawnAnchor) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	held, _ := u.HeldItems()
	_, glowstone := held.Item().(Glowstone)
	if glowstone && r.Charge < RespawnAnchorMaxCharge {
		ctx.SubtractFromCount(1)
		r.bumpCharge(pos, tx, 1, u)
		return true
	}
	if r.trySetSpawn(pos, tx, u) {
		return true
	}
	return glowstone
}

// trySetSpawn is RespawnAnchorBlock::_trySetSpawn.
func (r RespawnAnchor) trySetSpawn(pos cube.Pos, tx *world.Tx, u item.User) bool {
	if r.Charge <= 0 {
		return false
	}
	w := tx.World()
	if w.Dimension() != world.Nether {
		r.explode(pos, tx, u)
		return true
	}
	p, ok := u.(spawnPointUser)
	if !ok {
		return false
	}
	if dim, ok := w.PlayerSpawnDimension(p.UUID()); ok && dim == world.Nether && w.PlayerSpawn(p.UUID()) == pos {
		return false
	}
	w.SetPlayerSpawn(p.UUID(), pos)
	tx.PlaySound(pos.Vec3Centre(), sound.RespawnAnchorSetSpawn{})
	p.Messaget(chat.MessageRespawnAnchorSet)
	return true
}

// explode is RespawnAnchorBlock::_explode: 2+ water neighbours = explosion in water (no block damage).
func (r RespawnAnchor) explode(pos cube.Pos, tx *world.Tx, u item.User) {
	tx.SetBlock(pos, nil, nil)
	water := 0
	for _, f := range cube.Faces() {
		if l, ok := tx.Liquid(pos.Side(f)); ok {
			if _, isWater := l.(Water); isWater {
				water++
			}
		}
	}
	conf := ExplosionConfig{SpawnFire: true, NoBlockDamage: water > 1}
	if e, ok := u.(world.Entity); ok {
		conf.Source = e
	}
	conf.Explode(tx, world.BlockExplosionSource{Block: r, Pos: pos, ExplosionSize: 5})
}

// bumpCharge is RespawnAnchorBlock::_bumpCharge.
func (r RespawnAnchor) bumpCharge(pos cube.Pos, tx *world.Tx, delta int, src any) {
	r.Charge = min(max(r.Charge+delta, 0), RespawnAnchorMaxCharge)
	tx.SetBlock(pos, r, nil)
	if delta > 0 {
		tx.PlaySound(pos.Vec3Centre(), sound.RespawnAnchorCharge{})
	} else {
		tx.PlaySound(pos.Vec3Centre(), sound.RespawnAnchorDeplete{})
	}
	postBlockEvent(tx, gameevent.BlockChange, pos, src)
}

// InsertItem lets a hopper charge the anchor with glowstone (RespawnAnchorBlock::addItem).
func (r RespawnAnchor) InsertItem(h Hopper, pos cube.Pos, tx *world.Tx) bool {
	if r.Charge >= RespawnAnchorMaxCharge {
		return false
	}
	for slot, s := range h.inventory.Slots() {
		if _, ok := s.Item().(Glowstone); ok && !s.Empty() {
			_ = h.inventory.SetItem(slot, s.Grow(-1))
			r.bumpCharge(pos, tx, 1, nil)
			return true
		}
	}
	return false
}

// ExtractItem ...
func (RespawnAnchor) ExtractItem(Hopper, cube.Pos, *world.Tx) bool { return false }

// CanRespawnOn (RespawnAnchorBlock::canSpawnAt).
func (r RespawnAnchor) CanRespawnOn() bool { return r.Charge > 0 }

// UseCharge (RespawnAnchorBlock::notifySpawnedAt).
func (r RespawnAnchor) UseCharge(pos cube.Pos, tx *world.Tx) {
	if r.Charge > 0 {
		r.bumpCharge(pos, tx, -1, nil)
	}
}

// respawnAnchorOffsets: Java 26.3 RespawnAnchorBlock.RESPAWN_OFFSETS (fallback; Bedrock keeps the player's
// standing position, which Dragonfly does not store).
var respawnAnchorOffsets = func() []cube.Pos {
	horizontal := []cube.Pos{{0, 0, -1}, {-1, 0, 0}, {0, 0, 1}, {1, 0, 0}, {-1, 0, -1}, {1, 0, -1}, {-1, 0, 1}, {1, 0, 1}}
	var all []cube.Pos
	for _, dy := range []int{0, -1, 1} {
		for _, o := range horizontal {
			all = append(all, o.Add(cube.Pos{0, dy, 0}))
		}
	}
	return append(all, cube.Pos{0, 1, 0})
}()

// SafeSpawn returns the block a player respawning at the anchor stands in.
func (r RespawnAnchor) SafeSpawn(pos cube.Pos, tx *world.Tx) (cube.Pos, bool) {
	for _, o := range respawnAnchorOffsets {
		feet := pos.Add(o)
		if feet.OutOfBounds(tx.Range()) || feet.Side(cube.FaceUp).OutOfBounds(tx.Range()) {
			continue
		}
		if !respawnRoom(feet, tx) || !respawnRoom(feet.Side(cube.FaceUp), tx) {
			continue
		}
		below := feet.Side(cube.FaceDown)
		if !tx.Block(below).Model().FaceSolid(below, cube.FaceUp, tx) {
			continue
		}
		return feet, true
	}
	return cube.Pos{}, false
}

func respawnRoom(pos cube.Pos, tx *world.Tx) bool {
	if _, solid := tx.Block(pos).Model().(model.Solid); solid {
		return false
	}
	if l, ok := tx.Liquid(pos); ok {
		if _, lava := l.(Lava); lava {
			return false
		}
	}
	return true
}

// EncodeItem ...
func (RespawnAnchor) EncodeItem() (name string, meta int16) { return "minecraft:respawn_anchor", 0 }

// EncodeBlock ...
func (r RespawnAnchor) EncodeBlock() (string, map[string]any) {
	return "minecraft:respawn_anchor", map[string]any{"respawn_anchor_charge": int32(r.Charge)}
}

func allRespawnAnchors() (all []world.Block) {
	for c := 0; c <= RespawnAnchorMaxCharge; c++ {
		all = append(all, RespawnAnchor{Charge: c})
	}
	return
}
