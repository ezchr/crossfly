//go:build crossfly_full

package jpets

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// Small helpers over the Java mob base shared by every pet.

// itemName is the identifier of a stack's item ("minecraft:bone"), "" when
// empty.
func itemName(s item.Stack) string {
	if s.Empty() {
		return ""
	}
	name, _ := s.Item().EncodeItem()
	return name
}

// isCreative reports whether user has infinite materials
// (Player.hasInfiniteMaterials: creative).
func isCreative(user item.User) bool {
	if g, ok := user.(interface{ GameMode() world.GameMode }); ok {
		return g.GameMode().CreativeInventory()
	}
	return false
}

// consumeOne is ItemStack.consume(1, player) / usePlayerItem: one item less
// unless the user is in creative. It returns what is left in the hand.
func consumeOne(user item.User, held item.Stack) item.Stack {
	if isCreative(user) {
		return held
	}
	return held.Grow(-1)
}

// distSq is Entity.distanceToSqr(Entity): between the feet positions.
func distSq(a, b mgl64.Vec3) float64 { return a.Sub(b).LenSqr() }

// ownerOf is OwnableEntity.getOwner: the owner if it is in the mob's world.
func ownerOf(m *javamob.Mob) (world.Entity, bool) { return m.OwnerEntity() }

// isOwnedBy is TamableAnimal.isOwnedBy: e is the owner.
func isOwnedBy(m *javamob.Mob, e world.Entity) bool {
	if e == nil {
		return false
	}
	id, ok := m.OwnerUUID()
	return ok && e.H().UUID() == id
}

// spectator reports whether e is a player in spectator mode.
func spectator(e world.Entity) bool {
	p, ok := e.(*player.Player)
	return ok && p.GameMode() == world.GameModeSpectator
}

// Entity events (EntityEvent) both clients play: the hearts and smoke of a
// taming attempt (TamableAnimal/AbstractHorse.handleEntityEvent 7 and 6), the
// wet shake of a wolf (8, 56 cancels it).
type (
	tameSucceeded struct{}
	tameFailed    struct{}
	shakeWetness  struct{}
)

func (tameSucceeded) EntityAction()     {}
func (tameSucceeded) ActorEvent() uint8 { return packet.ActorEventTamingSucceeded }
func (tameFailed) EntityAction()        {}
func (tameFailed) ActorEvent() uint8    { return packet.ActorEventTamingFailed }
func (shakeWetness) EntityAction()      {}
func (shakeWetness) ActorEvent() uint8  { return packet.ActorEventShakeWetness }

// broadcastEvent is Level.broadcastEntityEvent: every viewer plays a.
func broadcastEvent(m *javamob.Mob, a world.EntityAction) {
	tx := m.Tx()
	if tx == nil {
		return
	}
	for _, v := range tx.Viewers(m.Position()) {
		v.ViewEntityAction(m, a)
	}
}

// reducedTickDelay is Goal.reducedTickDelay: ceil(ticks / 2), because goals
// that do not need every tick run every other tick.
func reducedTickDelay(ticks int) int { return (ticks + 1) / 2 }

// adjustedTickDelay is Goal.adjustedTickDelay for a goal without
// requiresUpdateEveryTick.
func adjustedTickDelay(ticks int) int { return reducedTickDelay(ticks) }

// mgl64Y is (0, y, 0).
func mgl64Y(y float64) mgl64.Vec3 { return mgl64.Vec3{0, y, 0} }

// itemStack is item.Stack (short for predicates).
type itemStack = item.Stack

// randomTeleport is LivingEntity.randomTeleport(x, y, z, false): drop to the
// first block with a floor below (not below the world), then move there if
// the mob fits and is not in a liquid.
func randomTeleport(m *javamob.Mob, to mgl64.Vec3) bool {
	tx := m.Tx()
	pos := cube.PosFromVec3(to)
	if r := tx.Range(); to[1] < float64(r[0]) || to[1] > float64(r[1]) {
		return false
	}
	found := false
	for pos[1] > tx.Range()[0] {
		below := pos.Side(cube.FaceDown)
		if len(tx.Block(below).Model().BBox(below, tx)) > 0 {
			found = true
			break
		}
		pos = below
	}
	if !found {
		return false
	}
	at := mgl64.Vec3{to[0], float64(pos[1]), to[2]}
	box := m.BBox().Translate(at.Sub(m.Position()))
	if path.Collides(tx, box) {
		return false
	}
	if _, liquid := tx.Liquid(pos); liquid {
		return false
	}
	m.Teleport(at)
	m.Navigation().Stop()
	return true
}
