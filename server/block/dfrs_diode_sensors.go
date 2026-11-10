//go:build crossfly_full

package block

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Sensors with redstone=dragonfly: pressure plates, tripwire, daylight detectors and target blocks with Bedrock
// Edition's rules (BDS 1.21.3). Buttons need nothing of their own: Bedrock's press times (wooden 30 game ticks, stone
// and polished blackstone 20) and arrows and tridents holding wooden buttons down are the same as Java Edition's.

// dfPressurePlateDetects reports whether an entity presses a plate (PressurePlateBlock::getSignalStrength): wooden and
// weighted plates any entity, stone and polished blackstone plates mobs (players and armour stands among them). An
// experience orb never does, nor a player passing through blocks.
func (p PressurePlate) dfPressurePlateDetects(e world.Entity) bool {
	if player, ok := e.(interface{ GameMode() world.GameMode }); ok && !player.GameMode().HasCollision() {
		return false
	}
	entityType := e.H().Type().EncodeEntity()
	if entityType == "minecraft:xp_orb" || pressurePlateIgnores[entityType] {
		return false
	}
	if p.Type.Wood() || p.Type.Weighted() {
		return true
	}
	if living, ok := e.(pressurePlateLivingEntity); ok {
		return !living.Dead()
	}
	return entityType == "minecraft:armor_stand"
}

// dfPressurePlateBox is the box entities press a plate in (BasePressurePlateBlock::getSensitiveAABB).
func dfPressurePlateBox(pos cube.Pos) cube.BBox {
	return cube.Box(0.0625, 0, 0.0625, 0.9375, 0.25, 0.9375).Translate(pos.Vec3())
}

// dfStringBox is the box entities trip a wire in (TripWireBlock's shape): an attached wire in the air a thin band,
// a wire hanging in the air half a block, one on the ground its bottom.
func (s String) dfStringBox(pos cube.Pos) cube.BBox {
	switch {
	case s.Attached && s.Suspended:
		return cube.Box(0, 0.0625, 0, 1, 0.15625, 1).Translate(pos.Vec3())
	case s.Suspended:
		return cube.Box(0, 0, 0, 1, 0.5, 1).Translate(pos.Vec3())
	}
	return cube.Box(0, 0, 0, 1, 0.09375, 1).Translate(pos.Vec3())
}

// dfStringRecheck is how often a tripped wire looks whether something is still in it.
const dfStringRecheck = time.Second / 2

// dfEntityInside trips the wire.
func (s String) dfEntityInside(pos cube.Pos, tx *world.Tx, e world.Entity) {
	if s.Powered || !tripwireDetects(e) || !entityIntersects(e, s.dfStringBox(pos)) {
		return
	}
	s.dfSetPowered(pos, tx, true)
}

// dfScheduledTick looks again whether something is still in a tripped wire.
func (s String) dfScheduledTick(pos cube.Pos, tx *world.Tx) {
	if !s.Powered {
		return
	}
	box := s.dfStringBox(pos)
	for e := range tx.EntitiesWithin(box.Grow(1)) {
		if tripwireDetects(e) && entityIntersects(e, box) {
			s.dfSchedule(pos, tx)
			return
		}
	}
	s.dfSetPowered(pos, tx, false)
}

// dfSetPowered trips or releases the wire and tells the hooks.
func (s String) dfSetPowered(pos cube.Pos, tx *world.Tx, powered bool) {
	s.Connections = calculateConnections(s, tx, pos)
	s.Powered = powered
	tx.SetBlock(pos, s, nil)
	s.dfUpdateSource(pos, tx)
	if powered {
		if now, ok := tx.Block(pos).(String); ok {
			now.dfSchedule(pos, tx)
		}
	}
}

// dfSchedule schedules the wire's recheck for both states a hook may give it before then (attached or not).
func (s String) dfSchedule(pos cube.Pos, tx *world.Tx) {
	s.Connections = calculateConnections(s, tx, pos)
	for _, a := range []bool{false, true} {
		v := s
		v.Attached = a
		tx.ScheduleBlockUpdate(pos, v, dfStringRecheck)
	}
}

// dfUpdateSource lets the hooks the wire is between recalculate the line (TripWireBlock::_updateSource), looking both
// ways: without Java Edition's shape updates a removed hook is only noticed from the far one.
func (s String) dfUpdateSource(pos cube.Pos, tx *world.Tx) {
	for _, d := range cube.Directions() {
		for i := 1; i < tripwireMaxLength; i++ {
			testPos := tripwireStep(pos, d, i)
			b := tx.Block(testPos)
			if hook, ok := b.(TripwireHook); ok {
				if hook.Facing == d.Opposite() {
					wire := s
					if now, ok := tx.Block(pos).(String); ok {
						wire = now
					}
					hook.calculateState(testPos, tx, false, true, i, &wire)
				}
				break
			}
			if _, ok := b.(String); !ok {
				break
			}
		}
	}
}

// dfNeighbourUpdate lets the hooks know when a wire or hook beside the wire went or came.
func (s String) dfNeighbourUpdate(pos, changed cube.Pos, tx *world.Tx) {
	if changed[1] != pos[1] {
		return
	}
	if _, ok := tx.Block(changed).(String); ok {
		return
	}
	if _, ok := tx.Block(pos).(String); ok {
		s.dfUpdateSource(pos, tx)
	}
}

// dfPlayerWillBreak: cut with shears, a wire is disarmed first and the hooks detach without a signal; broken
// otherwise, it trips first, so the hooks give a signal as they detach (TripWireBlock::playerWillDestroy).
func (s String) dfPlayerWillBreak(pos cube.Pos, held item.Stack, tx *world.Tx) {
	if _, ok := held.Item().(item.Shears); ok {
		s.Disarmed, s.Powered = true, false
	} else {
		s.Powered = true
	}
	s.Connections = calculateConnections(s, tx, pos)
	tx.SetBlock(pos, s, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true})
	s.dfUpdateSource(pos, tx)
}

// bedrockDaylightPower is a daylight detector's signal (DaylightDetectorBlock::updateSignalStrength): the sky light
// at it less the sky darkness, which for a normal detector is weighed by the angle of the sun, and taken from 15 for an
// inverted one. cycle is false if the daylight cycle is off.
func bedrockDaylightPower(sky, timeOfDay int, cycle, inverted bool, rain, thunder float32) int {
	partial := float32(1)
	if !cycle {
		partial = 0
	}
	f := (float32(((timeOfDay%24000)+24000)%24000) + partial) / 24000
	g := f - 0.25
	if f < 0.25 {
		g = f + 0.75
	}
	if g > 1 {
		g--
	}
	celestial := g + ((0.5-float32(math.Cos(float64(g)*math.Pi))*0.5)-g)/3
	angle := celestial * 2 * math.Pi

	light := 1 - min(max(0.5-2*float32(math.Cos(float64(angle))), 0), 1)
	light *= 1 - rain*5/16
	light *= 1 - thunder*5/16
	darken := int((1 - light) * 11)

	s := sky - darken
	if inverted {
		return world.ClampRedstonePower(15 - s)
	}
	if s <= 0 {
		return 0
	}
	target := float32(0)
	if angle >= math.Pi {
		target = 2 * math.Pi
	}
	angle += (target - angle) * 0.2
	return world.ClampRedstonePower(int(math.Round(math.Cos(float64(angle)) * float64(s))))
}

// bedrockTargetStrength is a target block's signal for a hit (TargetBlock::_processHitByProjectileAtPos): from 15 at
// the middle of the face hit to 1 at its edge, by the distance of the hit from the middle.
func bedrockTargetStrength(face cube.Face, hit mgl64.Vec3) int {
	var d [3]float64
	for i, v := range hit {
		d[i] = math.Abs(v - math.Floor(v) - 0.5)
	}
	var u, v float64
	switch face.Axis() {
	case cube.X:
		u, v = d[1], d[2]
	case cube.Y:
		u, v = d[0], d[2]
	default:
		u, v = d[0], d[1]
	}
	half := math.Sqrt(0.5)
	return min(max(int((half-math.Sqrt(u*u+v*v))*16/half), 1), 15)
}

// bedrockTargetTicks is how long a target block gives its signal: 20 game ticks for arrows and tridents, 8 for
// other projectiles.
func bedrockTargetTicks(projectile world.Entity) int {
	switch projectile.H().Type().EncodeEntity() {
	case "minecraft:arrow", "minecraft:spectral_arrow", "minecraft:thrown_trident":
		return 20
	}
	return 8
}

// dfPlaced lets the hooks know a wire was placed.
func (s String) dfPlaced(pos cube.Pos, tx *world.Tx) {
	if now, ok := tx.Block(pos).(String); ok {
		now.dfUpdateSource(pos, tx)
	}
}

// dfProjectileHit gives the target block's signal for a hit. A new hit starts the signal again, at its own strength
// (Bedrock cancels the waiting tick).
func (t Target) dfProjectileHit(pos cube.Pos, tx *world.Tx, projectile world.Entity, face cube.Face, hit mgl64.Vec3) {
	ticks := bedrockTargetTicks(projectile)
	t.Power = bedrockTargetStrength(face, hit)
	t.Expires = tx.CurrentTick() + int64(ticks)
	tx.SetBlock(pos, t, nil)
	tx.ScheduleBlockUpdate(pos, t, time.Duration(ticks)*time.Second/20)
}
