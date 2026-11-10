//go:build crossfly_full

package animal

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// Bats hanging from ceilings are hardcoded in BDS (class Bat); bat.json has
// no component for it, so it is gated by identifier. The resting state is
// the Bedrock actor flag RESTING (23), which both clients draw as a bat
// hanging upside down.
const batID = "minecraft:bat"

// batWakeDistance is the distance of a player that wakes a resting bat
// (Dimension::fetchNearestInteractablePlayer(bat, 4)).
const batWakeDistance = 4

func init() {
	mob.RegisterInitHook(mob.InitHook{Name: "animal/bat_resting", Init: batInit})
	mob.RegisterSystem(mob.System{Name: "animal/bat_ai", Stage: mob.StageGoals, Order: 301, Tick: batServerAiStep})
	mob.RegisterSystem(mob.System{Name: "animal/bat_hang", Stage: mob.StageTravel, Order: 32, Tick: batPostNormalTick})
	mob.RegisterHurtHook(mob.HurtHook{Name: "animal/bat_hurt", Hurt: batHurt})
	mob.RegisterNBTHook(mob.NBTHook{Name: "animal/bat_flags", Encode: encodeBatFlags, Decode: decodeBatFlags})
}

// batWasRestingKey keeps the bat's mWasResting, by which
// Bat::postNormalTick notices the bat taking off.
type batWasRestingKey struct{}

// batInit is the Bat constructor: a new bat rests (and was resting).
func batInit(m *mob.Mob) {
	if m.Identifier() != batID {
		return
	}
	m.SetActorFlag(protocol.EntityDataFlagResting, true)
	m.SetData(batWasRestingKey{}, true)
}

// BatResting reports whether a bat hangs from the ceiling.
func BatResting(m *mob.Mob) bool { return m.ActorFlag(protocol.EntityDataFlagResting) }

// batRestingBlockAbove is Bat::hasRestingBlockAbove: the block over the
// bat's block position is solid and blocking (BlockSource::
// isSolidBlockingBlock), here: its bottom face is solid.
func batRestingBlockAbove(tx *world.Tx, pos mgl64.Vec3) bool {
	p := cube.PosFromVec3(pos).Side(cube.FaceUp)
	return tx.Block(p).Model().FaceSolid(p, cube.FaceDown, tx)
}

// batServerAiStep is Bat::newServerAiStep: a resting bat wakes up when the
// block above it goes or a player (not a spectator) comes within 4 blocks
// (of its eyes: a Bedrock player's position), and turns its head now and
// then (1 in 200 ticks); a flying bat hangs up 1 in 100 ticks when it is
// under a ceiling.
func batServerAiStep(m *mob.Mob) {
	if m.Identifier() != batID {
		return
	}
	tx, pos, r := m.Tx(), m.Position(), m.Rand()
	if !BatResting(m) {
		if r.IntN(100) == 0 && batRestingBlockAbove(tx, pos) {
			m.SetActorFlag(protocol.EntityDataFlagResting, true)
		}
		return
	}
	if !batRestingBlockAbove(tx, pos) {
		m.SetActorFlag(protocol.EntityDataFlagResting, false)
		return
	}
	if r.IntN(200) == 0 {
		m.SetHeadYaw(r.Float64() * 360)
	}
	for p := range tx.Players() {
		if kit.VisiblePlayer(p) && kit.DistSq(kit.EyePos(p), pos) < batWakeDistance*batWakeDistance {
			m.SetActorFlag(protocol.EntityDataFlagResting, false)
			return
		}
	}
}

// batPostNormalTick is Bat::postNormalTick: a resting bat stands still,
// hanging with its top at the ceiling (y = floor(y) + 1 - height); a flying
// one keeps 0.6 of its vertical speed each tick and plays the takeoff sound
// on the tick it leaves the ceiling.
func batPostNormalTick(m *mob.Mob) {
	if m.Identifier() != batID {
		return
	}
	resting := BatResting(m)
	v, _ := m.Data(batWasRestingKey{})
	wasResting, _ := v.(bool)
	m.SetData(batWasRestingKey{}, resting)
	if resting {
		pos := m.Pos()
		pos[1] = math.Floor(pos[1]) + 1 - m.BBox().Height()
		m.SetVelocity(mgl64.Vec3{})
		m.SetPosition(pos)
		m.ResetFallDistance()
		return
	}
	vel := m.Vel()
	vel[1] *= 0.6
	m.SetVelocity(vel)
	if wasResting {
		m.PlaySound("takeoff")
	}
}

// batHurt is Bat::_hurt: a hit wakes a resting bat.
func batHurt(m *mob.Mob, _ float64, _ world.DamageSource) {
	if m.Identifier() == batID && BatResting(m) {
		m.SetActorFlag(protocol.EntityDataFlagResting, false)
	}
}

// encodeBatFlags is Bat::addAdditionalSaveData: BatFlags, 1 when resting.
func encodeBatFlags(m *mob.Mob) map[string]any {
	if m.Identifier() != batID {
		return nil
	}
	var f uint8
	if BatResting(m) {
		f = 1
	}
	return map[string]any{"BatFlags": f}
}

// decodeBatFlags is Bat::readAdditionalSaveData.
func decodeBatFlags(m *mob.Mob, d map[string]any) {
	if m.Identifier() != batID {
		return
	}
	if f, ok := d["BatFlags"].(uint8); ok {
		m.SetActorFlag(protocol.EntityDataFlagResting, f != 0)
		m.SetData(batWasRestingKey{}, f != 0)
	}
}
