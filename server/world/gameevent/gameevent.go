// Package gameevent is the game event (vibration) dispatch of BDS
// (GameEventRegistry, BlockSource::postGameEvent, GameEventDispatcher): what
// players, mobs and blocks do posts a game event at a position, and the
// vibration listeners near it (wardens, sculk sensors, sculk shriekers) hear
// it.
//
// Posting costs nothing until a listener exists: Post returns at once while
// no listener is registered (Crossfly Core, or Crossfly Full with the mobs
// off), and also in worlds that hold no listener.
package gameevent

import (
	"math"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Event is a BDS GameEvent: its GameEventRegistry name and id, and its
// vibration frequency (VibrationListenerUtils::getGameEventFrequency; 0: no
// listener reacts to it as a plain vibration).
type Event struct {
	Name      string
	ID        int32
	Frequency int
	// Sneaky: not heard from a sneaking entity
	// (VibrationListenerUtils::ignoreVibrationIfSneaking).
	Sneaky bool
}

// The game events, with their BDS ids and frequencies. The ids 40 to 54 are
// resonate_1 to resonate_15 (Resonate).
var (
	BlockActivate               = Event{Name: "block_activate", ID: 1, Frequency: 10}
	BlockAttach                 = Event{Name: "block_attach", ID: 2, Frequency: 10}
	BlockChange                 = Event{Name: "block_change", ID: 3, Frequency: 11}
	BlockClose                  = Event{Name: "block_close", ID: 4, Frequency: 9}
	BlockDeactivate             = Event{Name: "block_deactivate", ID: 5, Frequency: 9}
	BlockDestroy                = Event{Name: "block_destroy", ID: 6, Frequency: 12}
	BlockDetach                 = Event{Name: "block_detach", ID: 7, Frequency: 9}
	BlockOpen                   = Event{Name: "block_open", ID: 8, Frequency: 10}
	BlockPlace                  = Event{Name: "block_place", ID: 9, Frequency: 13}
	ContainerClose              = Event{Name: "container_close", ID: 10, Frequency: 9}
	ContainerOpen               = Event{Name: "container_open", ID: 11, Frequency: 10}
	DispenseFail                = Event{Name: "dispense_fail", ID: 12, Frequency: 10}
	Drink                       = Event{Name: "drink", ID: 13, Frequency: 8}
	Eat                         = Event{Name: "eat", ID: 14, Frequency: 8}
	ElytraGlide                 = Event{Name: "elytra_glide", ID: 15, Frequency: 4}
	EntityDamage                = Event{Name: "entity_damage", ID: 16, Frequency: 7}
	EntityDie                   = Event{Name: "entity_die", ID: 17, Frequency: 15}
	EntityDismount              = Event{Name: "entity_dismount", ID: 18, Frequency: 5}
	EntityInteract              = Event{Name: "entity_interact", ID: 19, Frequency: 6}
	EntityMount                 = Event{Name: "entity_mount", ID: 20, Frequency: 6}
	EntityMove                  = Event{Name: "entity_move", ID: 21, Frequency: 1, Sneaky: true}
	EntityPlace                 = Event{Name: "entity_place", ID: 22, Frequency: 14}
	EntityRoar                  = Event{Name: "entity_roar", ID: 23, Frequency: 4}
	EntityAct                   = Event{Name: "entity_act", ID: 24, Frequency: 4}
	Equip                       = Event{Name: "equip", ID: 25, Frequency: 5}
	Explode                     = Event{Name: "explode", ID: 26, Frequency: 15}
	Flap                        = Event{Name: "flap", ID: 27, Frequency: 1}
	FluidPickup                 = Event{Name: "fluid_pickup", ID: 28, Frequency: 12}
	FluidPlace                  = Event{Name: "fluid_place", ID: 29, Frequency: 13}
	HitGround                   = Event{Name: "hit_ground", ID: 30, Frequency: 2, Sneaky: true}
	ItemInteractFinish          = Event{Name: "item_interact_finish", ID: 31, Frequency: 3}
	ItemInteractStart           = Event{Name: "item_interact_start", ID: 32}
	LightningStrike             = Event{Name: "lightning_strike", ID: 33, Frequency: 14}
	NoteBlockPlay               = Event{Name: "note_block_play", ID: 34, Frequency: 10}
	PistonContract              = Event{Name: "piston_contract", ID: 35, Frequency: 9}
	PistonExtend                = Event{Name: "piston_extend", ID: 36, Frequency: 10}
	PrimeFuse                   = Event{Name: "prime_fuse", ID: 37, Frequency: 10}
	ProjectileLand              = Event{Name: "projectile_land", ID: 38, Frequency: 2}
	ProjectileShoot             = Event{Name: "projectile_shoot", ID: 39, Frequency: 3, Sneaky: true}
	SculkTouch                  = Event{Name: "sculk_touch", ID: 55, Frequency: 1}
	SculkSensorTendrilsClicking = Event{Name: "sculk_sensor_tendrils_clicking", ID: 56}
	Shear                       = Event{Name: "shear", ID: 57, Frequency: 6}
	Shriek                      = Event{Name: "shriek", ID: 58}
	Splash                      = Event{Name: "splash", ID: 59, Frequency: 2}
	Swim                        = Event{Name: "swim", ID: 60, Frequency: 1, Sneaky: true}
	Teleport                    = Event{Name: "teleport", ID: 61, Frequency: 14}
	Unequip                     = Event{Name: "unequip", ID: 62, Frequency: 4}
)

// resonances are resonate_1 to resonate_15 (GameEventRegistry::resonate1 to resonate15, ids 40 to 54):
// an amethyst block next to a sculk sensor that receives a vibration of frequency n re-emits it as
// resonate_n, whose frequency is n.
var resonances = func() (r [15]Event) {
	for i := range r {
		n := i + 1
		r[i] = Event{Name: "resonate_" + strconv.Itoa(n), ID: int32(39 + n), Frequency: n}
	}
	return r
}()

// Resonate returns resonate_n for a vibration frequency n (1 to 15), and false for any other.
func Resonate(n int) (Event, bool) {
	if n < 1 || n > len(resonances) {
		return Event{}, false
	}
	return resonances[n-1], true
}

// Context is who made a game event (BDS GameEventContext).
type Context struct {
	// Source is the entity that made it: the one moving, hurt, eating, the
	// projectile that landed. It may be nil (a container opened by a
	// hopper).
	Source world.Entity
	// Owner is the entity behind it: the shooter of a projectile, the
	// attacker of an entity hurt.
	Owner world.Entity
	// Damped: the source has minecraft:vibration_damper (a warden), so
	// sculk does not hear it.
	Damped bool
}

// Listener hears every game event posted in a world that has listeners.
// Listeners check their own range.
type Listener func(tx *world.Tx, ev Event, pos mgl64.Vec3, ctx Context)

var (
	hooks []Listener
	// worlds counts the vibration listeners (wardens, sculk blocks) of
	// each world: Post returns at once in a world without any.
	worlds sync.Map // *world.World -> *atomic.Int64
)

// Register adds l. Call it from an init function, or before the server
// starts: the list is read without locks.
func Register(l Listener) { hooks = append(hooks, l) }

// Enabled reports whether any listener is registered (Crossfly Full).
func Enabled() bool { return len(hooks) != 0 }

// Track changes the number of vibration listeners of w by delta. A listener
// counts itself in when it starts listening and out when it stops.
func Track(w *world.World, delta int) {
	if w == nil || delta == 0 {
		return
	}
	v, _ := worlds.LoadOrStore(w, new(atomic.Int64))
	if n := v.(*atomic.Int64).Add(int64(delta)); n < 0 {
		v.(*atomic.Int64).Store(0)
	}
}

// Active reports whether the world of tx has vibration listeners, so that
// a game event posted there may be heard.
func Active(tx *world.Tx) bool {
	if len(hooks) == 0 || tx == nil {
		return false
	}
	v, ok := worlds.Load(tx.World())
	return ok && v.(*atomic.Int64).Load() > 0
}

// Post posts ev at pos, made by src (BlockSource::postGameEvent).
func Post(tx *world.Tx, ev Event, pos mgl64.Vec3, src world.Entity) {
	PostContext(tx, ev, pos, Context{Source: src})
}

// PostContext posts ev at pos with ctx.
func PostContext(tx *world.Tx, ev Event, pos mgl64.Vec3, ctx Context) {
	if !Active(tx) {
		return
	}
	for _, h := range hooks {
		h(tx, ev, pos, ctx)
	}
}

// projectileOwner is set by package entity: it reports whether e is a projectile and returns its shooter
// (nil if it has none or the shooter is gone).
var projectileOwner func(tx *world.Tx, e world.Entity) (world.Entity, bool)

// SetProjectileOwner sets the function that tells projectiles and their shooters apart (package entity
// calls it from an init function).
func SetProjectileOwner(f func(tx *world.Tx, e world.Entity) (world.Entity, bool)) {
	projectileOwner = f
}

// ProjectileOwner returns the shooter of e if e is a projectile with one (ok false otherwise).
func ProjectileOwner(tx *world.Tx, e world.Entity) (world.Entity, bool) {
	if e == nil || projectileOwner == nil {
		return nil, false
	}
	o, projectile := projectileOwner(tx, e)
	return o, projectile && o != nil
}

// IsProjectile reports whether e is a projectile (BDS ActorType Projectile).
func IsProjectile(tx *world.Tx, e world.Entity) bool {
	if e == nil || projectileOwner == nil {
		return false
	}
	_, projectile := projectileOwner(tx, e)
	return projectile
}

// Sneaking reports whether e sneaks, the shooter for a projectile (owner):
// sneaky events (steps, swims, landing, shooting) of sneaking entities are
// not heard.
func Sneaking(ev Event, ctx Context) bool {
	if !ev.Sneaky {
		return false
	}
	e := ctx.Source
	if ctx.Owner != nil {
		e = ctx.Owner
	}
	s, ok := e.(interface{ Sneaking() bool })
	return ok && s.Sneaking()
}

// Movement is the movement tracking of one entity (BDS
// GameEventMovementTrackingComponent, GameEventMovementTrackingSystem::
// _trackMovement): it posts a step (entity_move), or a swim in water or
// lava, each time the entity has travelled a further distance unit, and
// a flap every 10 ticks while it flies (for those that flap).
type Movement struct {
	last      mgl64.Vec3
	dist      float64
	nextStep  float64
	nextFlap  int64
	peak      float64
	started   bool
	wasGround bool
}

// MoveState is what Movement needs to know about the entity this tick.
type MoveState struct {
	// Pos is the entity's position.
	Pos mgl64.Vec3
	// Tick is the current tick (flaps are 10 ticks apart).
	Tick int64
	// OnGround, InLiquid (water or lava), Climbing (ladder, vines,
	// scaffolding, powder snow, cobweb): BDS's conditions for steps and
	// swims; flying is none of them.
	OnGround, InLiquid, Climbing bool
	// EmitMove, EmitSwim, EmitFlap are game_event_movement_tracking's
	// switches (players: steps and swims, no flaps).
	EmitMove, EmitSwim, EmitFlap bool
}

// Track follows the entity's movement and returns the event to post, if
// any (ok false: none).
func (m *Movement) Track(s MoveState) (Event, bool) {
	if !m.started {
		m.started, m.last, m.wasGround = true, s.Pos, s.OnGround
		return Event{}, false
	}
	// BlockLegacy::onFallOnBase: landing after a fall of any height (a
	// jump too) hits the ground.
	landed := s.OnGround && !m.wasGround && m.peak > s.Pos[1]
	switch {
	case s.OnGround || s.InLiquid || s.Climbing:
		m.peak = s.Pos[1]
	case m.wasGround:
		m.peak = max(m.last[1], s.Pos[1])
	default:
		m.peak = max(m.peak, s.Pos[1])
	}
	m.wasGround = s.OnGround || s.InLiquid || s.Climbing
	if landed {
		m.last = s.Pos
		return HitGround, true
	}
	if s.OnGround || s.InLiquid || s.Climbing {
		d := s.Pos.Sub(m.last)
		vertical := 0.0
		if s.InLiquid || s.Climbing {
			vertical = 9 * d[1] * d[1]
		}
		m.dist += math.Sqrt((d[0]*d[0]+d[2]*d[2])*9 + vertical)
		m.last = s.Pos
		if m.dist < m.nextStep {
			return Event{}, false
		}
		// Actor::getNextStep: the next whole distance unit.
		m.nextStep = float64(int(m.dist)) + 1
		switch {
		case s.InLiquid && s.EmitSwim:
			return Swim, true
		case !s.InLiquid && s.EmitMove:
			return EntityMove, true
		}
		return Event{}, false
	}
	m.last = s.Pos
	if s.Tick < m.nextFlap {
		return Event{}, false
	}
	m.nextFlap = s.Tick + 10
	if s.EmitFlap {
		return Flap, true
	}
	return Event{}, false
}
