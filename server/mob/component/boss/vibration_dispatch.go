//go:build crossfly_full

package boss

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/gameevent"
	"github.com/go-gl/mathgl/mgl64"
)

// The warden hears the game events of the world (package gameevent) besides what its own scan notices
// (steps, swims, landing, projectiles: VibrationListener.scan), and mobs post the game events of their
// movement for the sculk blocks.
func init() {
	for _, ev := range []gameevent.Event{
		gameevent.BlockActivate, gameevent.BlockAttach, gameevent.BlockChange, gameevent.BlockClose,
		gameevent.BlockDeactivate, gameevent.BlockDestroy, gameevent.BlockDetach, gameevent.BlockOpen,
		gameevent.BlockPlace, gameevent.ContainerClose, gameevent.ContainerOpen, gameevent.DispenseFail,
		gameevent.Drink, gameevent.Eat, gameevent.ElytraGlide, gameevent.EntityDamage, gameevent.EntityDie,
		gameevent.EntityDismount, gameevent.EntityInteract, gameevent.EntityMount, gameevent.EntityMove,
		gameevent.EntityPlace, gameevent.EntityRoar, gameevent.EntityAct, gameevent.Equip, gameevent.Explode,
		gameevent.Flap, gameevent.FluidPickup, gameevent.FluidPlace, gameevent.HitGround,
		gameevent.ItemInteractFinish, gameevent.LightningStrike, gameevent.NoteBlockPlay,
		gameevent.PistonContract, gameevent.PistonExtend, gameevent.PrimeFuse, gameevent.ProjectileLand,
		gameevent.ProjectileShoot, gameevent.SculkTouch, gameevent.Shear, gameevent.Splash, gameevent.Swim,
		gameevent.Teleport, gameevent.Unequip,
	} {
		vibrationFrequency[ev.Name] = ev.Frequency
	}
	gameevent.Register(hearMobListeners)
	world.RegisterBlockBreakHook(func(tx *world.Tx, pos cube.Pos, _ world.Block, breaker world.Entity) {
		gameevent.Post(tx, gameevent.BlockDestroy, pos.Vec3Centre(), breaker)
	})
	mob.RegisterSystem(mob.System{Name: "boss/vibration/move", Stage: mob.StageEnvironment, Order: 60, Tick: trackMovement})
	block.SetWardenSummoner(TrySummonWarden)
}

// scanned are the game events the warden's listener finds by itself (VibrationListener.scan): it takes
// them from the scan only, so that none is heard twice.
var scanned = map[string]bool{
	gameevent.EntityMove.Name: true, gameevent.Swim.Name: true, gameevent.HitGround.Name: true,
	gameevent.ProjectileLand.Name: true, gameevent.ProjectileShoot.Name: true,
}

// hearMobListeners offers a game event to the mobs with a vibration_listener in the world.
func hearMobListeners(tx *world.Tx, ev gameevent.Event, pos mgl64.Vec3, ctx gameevent.Context) {
	if scanned[ev.Name] || ev.Frequency == 0 {
		return
	}
	handles := listenersIn(tx)
	if len(handles) == 0 {
		return
	}
	if gameevent.Sneaking(ev, ctx) {
		return
	}
	vib := Vibration{Event: ev.Name, Pos: pos}
	if ctx.Source != nil {
		vib.Source = ctx.Source.H()
	}
	if ctx.Owner != nil {
		vib.Owner = ctx.Owner.H()
	}
	for _, h := range handles {
		e, ok := h.Entity(tx)
		if !ok {
			forgetListener(tx, h)
			continue
		}
		lm, ok := mob.Of(e)
		if !ok {
			continue
		}
		if l, ok := ListenerOf(lm); ok {
			l.Hear(lm, vib) // Hear checks the range and the source.
		}
	}
}

// moveKey keeps a mob's movement tracking.
type moveKey struct{}

// trackMovement is GameEventMovementTrackingSystem for mobs: steps, swims and (for those that flap)
// flaps, as game_event_movement_tracking says (steps and swims by default). Nothing runs in a world
// without vibration listeners.
func trackMovement(m *mob.Mob) {
	tx := m.Tx()
	if !gameevent.Active(tx) {
		return
	}
	if m.OnGround() {
		stepOnSculk(tx, m)
	}
	var mv *gameevent.Movement
	if v, ok := m.Data(moveKey{}); ok {
		mv = v.(*gameevent.Movement)
	} else {
		mv = &gameevent.Movement{}
		m.SetData(moveKey{}, mv)
	}
	s := gameevent.MoveState{Pos: m.Position(), Tick: m.Ticks(), OnGround: m.OnGround(), EmitMove: true, EmitSwim: true}
	if c, ok := mob.ComponentOf[*MovementTracking](m); ok {
		s.EmitMove, s.EmitSwim, s.EmitFlap = c.EmitMove, c.EmitSwim, c.EmitFlap
	}
	p := m.Physics()
	s.InLiquid = p.InWater || p.InLava
	s.Climbing = !s.InLiquid && !s.OnGround && !airborne(m)
	ev, ok := mv.Track(s)
	if !ok {
		return
	}
	gameevent.PostContext(tx, ev, m.Position(), gameevent.Context{Source: m, Damped: m.HasComponent("minecraft:vibration_damper")})
}

// TrySummonWarden is SculkShriekerBlockActorInternal::_trySummonWarden: up to 20 tries at a random spot
// within 5 blocks across and 6 up or down from pos for one where a warden fits standing on a full block
// top, which then emerges (minecraft:spawn_emerging) with the warden's agitated sound.
//
// The spot search follows Java's SpawnUtil.trySpawnMob with Strategy.ON_TOP_OF_COLLIDER (Java fallback:
// the BDS dump has no body for MobSpawnUtils::trySpawnMob, which BDS calls with the same 20, 5 and 6).
func TrySummonWarden(tx *world.Tx, pos cube.Pos) bool {
	for range 20 {
		x, z := pos[0]+rand.IntN(11)-5, pos[2]+rand.IntN(11)-5
		at, ok := wardenSpot(tx, cube.Pos{x, pos[1] + 6, z}, 6)
		if !ok {
			continue
		}
		feet := mgl64.Vec3{float64(at[0]) + 0.5, float64(at[1]), float64(at[2]) + 0.5}
		box := cube.Box(-0.45, 0, -0.45, 0.45, 2.9, 0.45).Translate(feet)
		if !boxFree(tx, box) {
			continue
		}
		m, err := mob.Spawn(tx, WardenID, feet, mob.SpawnOptions{Event: "minecraft:spawn_emerging"})
		if err != nil {
			return false
		}
		m.PlaySound("agitated")
		return true
	}
	return false
}

// wardenSpot is SpawnUtil.moveToPossibleSpawnPosition: from top down to yRange below pos, the first
// block with a full top face under a block with no collision.
func wardenSpot(tx *world.Tx, top cube.Pos, yRange int) (cube.Pos, bool) {
	for y := top[1]; y >= top[1]-2*yRange; y-- {
		below, at := cube.Pos{top[0], y - 1, top[2]}, cube.Pos{top[0], y, top[2]}
		if hasCollision(tx, at) || !fullTop(tx, below) {
			continue
		}
		return at, true
	}
	return cube.Pos{}, false
}

func hasCollision(tx *world.Tx, pos cube.Pos) bool {
	return len(tx.Block(pos).Model().BBox(pos, tx)) != 0
}

func fullTop(tx *world.Tx, pos cube.Pos) bool {
	return tx.Block(pos).Model().FaceSolid(pos, cube.FaceUp, tx)
}

// boxFree reports whether no block collides with box and no living entity is in it (Level::noCollision,
// Mob::checkSpawnObstruction).
func boxFree(tx *world.Tx, box cube.BBox) bool {
	lo, hi := cube.PosFromVec3(box.Min()), cube.PosFromVec3(box.Max())
	for x := lo[0]; x <= hi[0]; x++ {
		for y := lo[1]; y <= hi[1]; y++ {
			for z := lo[2]; z <= hi[2]; z++ {
				p := cube.Pos{x, y, z}
				for _, bb := range tx.Block(p).Model().BBox(p, tx) {
					if bb.Translate(p.Vec3()).IntersectsWith(box) {
						return false
					}
				}
			}
		}
	}
	for e := range tx.EntitiesWithin(box) {
		if IsLiving(e) {
			return false
		}
	}
	return true
}

// stepOnSculk is BlockLegacy::onStandOn for a mob on the ground: a sculk sensor under it posts sculk_touch
// and a sculk shrieker shrieks for the player riding it (SculkSensorBlock::onStandOn,
// SculkShriekerBlock::onStandOn). Players and other entities do this from their own movement; for mobs it
// runs with the movement tracking, only in worlds with vibration listeners. Of the blocks under the mob,
// the first that reacts to standing on it is the one stood on, as for the other entities
// (entity.StepOnBlock).
func stepOnSculk(tx *world.Tx, m *mob.Mob) {
	box := m.H().Type().BBox(m).Translate(m.Position())
	y := int(math.Floor(box.Min()[1] - 0.0001))
	h := box.Grow(-0.0001)
	lo, hi := cube.PosFromVec3(h.Min()), cube.PosFromVec3(h.Max())
	for x := lo[0]; x <= hi[0]; x++ {
		for z := lo[2]; z <= hi[2]; z++ {
			pos := cube.Pos{x, y, z}
			switch b := tx.Block(pos).(type) {
			case block.SculkStepper:
				b.EntityStepOn(pos, tx, m)
				return
			case block.EntityStepper:
				return
			}
		}
	}
}
