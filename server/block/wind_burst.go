package block

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/gameevent"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// WindBurst is the burst of a wind charge: an explosion that breaks nothing, starts no fire, hurts nobody,
// works under water, ignores block resistance, toggles doors, trapdoors, fence gates, levers, buttons,
// bells and candles in its rays and pushes every entity in range away from its centre, the one who threw
// it too (the wind charge jump).
//
// This is BDS 1.21.3 WindBurstUtility::burst, which sets up an Explosion with setFire(0),
// setBreaking(0), setAllowUnderwater(1), setCanToggleBlocks(1), setShouldTakeDamage(0),
// setIgnoreBlockResistance(1), the particle, sound and knockback scaling of the definition and the custom
// knockback unity_56fb12b8::calculateKnockback, and runs Explosion::explode. The numbers of a player's wind
// charge come from the 1.26.50 behaviour pack wind_charge_projectile.json (minecraft:explode).
type WindBurst struct {
	// Power is the radius of the burst (explode power, WindBurstDefinition radius). Entities within twice
	// that distance are pushed.
	Power float64
	// KnockbackScaling multiplies the push (knockback_scaling).
	KnockbackScaling float64
	// NegatesFallDamage marks players pushed by the burst so that their fall damage only counts what they
	// fall below the height they were pushed at (negates_fall_damage).
	NegatesFallDamage bool
	// Source is the entity that burst (the wind charge). It is not pushed, and blocks toggled by the burst
	// are toggled by it.
	Source world.Entity
	// SourceVelocity is the velocity Source had when it burst. A burst inside a solid block steps back along it.
	SourceVelocity mgl64.Vec3
	// Sound and Particle are played at the centre of the burst. Nothing is played if nil.
	Sound    world.Sound
	Particle world.Particle
	// RandSource is the source of the ray strengths. If nil, one seeded with the time is used.
	RandSource rand.Source
}

// WindBurstLauncher is an entity that takes the push of a wind burst itself (a player): push is the velocity
// the burst adds before the entity's knockback resistance.
type WindBurstLauncher interface {
	WindBurstLaunch(push mgl64.Vec3, negatesFallDamage bool)
}

// WindBurstEntityInfo, if set, returns the eye position and knockback resistance of entities the block
// package cannot see into (Crossfly Full: the mobs). ok is false for entities it does not know.
var WindBurstEntityInfo func(e world.Entity) (eye mgl64.Vec3, knockbackResistance float64, ok bool)

// WindBurstSource is the world.ExplosionSource a wind burst passes to world.Handler.HandleExplosion. A
// handler can tell a wind burst from other explosions by this type. Its blocks are the blocks the burst
// toggles; removing one keeps it from toggling.
type WindBurstSource struct {
	Pos    mgl64.Vec3
	Radius float64
	Entity world.Entity
}

// Position ...
func (s WindBurstSource) Position() mgl64.Vec3 { return s.Pos }

// Size ...
func (s WindBurstSource) Size() float64 { return s.Radius }

// windToggler is a block a wind burst toggles (BDS BlockLegacy::_onHitByActivatingAttack).
type windToggler interface {
	windBurstToggle(pos cube.Pos, tx *world.Tx, src world.Entity)
}

// Burst lets the wind burst go off at pos.
func (w WindBurst) Burst(tx *world.Tx, pos mgl64.Vec3) {
	if !(w.Power > 0) || math.IsInf(w.Power, 0) || !finiteVec(pos) {
		return
	}
	if w.RandSource == nil {
		t := uint64(time.Now().UnixNano())
		w.RandSource = rand.NewPCG(t, t)
	}
	pos = w.stepOutOfBlock(tx, pos)
	src := WindBurstSource{Pos: pos, Radius: w.Power, Entity: w.Source}
	gameevent.Post(tx, gameevent.Explode, pos, w.Source)

	blocks := w.toggledBlocks(tx, pos, rand.New(w.RandSource))
	d := w.Power * 2
	box := cube.Box(pos[0]-d-1, pos[1]-d-1, pos[2]-d-1, pos[0]+d+1, pos[1]+d+1, pos[2]+d+1)
	entities := make([]world.Entity, 0, 8)
	for e := range tx.EntitiesWithin(box) {
		if w.Source != nil && e.H() == w.Source.H() {
			continue
		}
		entities = append(entities, e)
	}

	ctx := tx.Event()
	itemDropChance, spawnFire := 0.0, false
	if tx.World().Handler().HandleExplosion(ctx, src, &entities, &blocks, &itemDropChance, &spawnFire); ctx.Cancelled() {
		return
	}

	for _, e := range entities {
		w.push(tx, pos, e)
	}
	if w.Particle != nil {
		tx.AddParticle(pos, w.Particle)
	}
	if w.Sound != nil {
		tx.PlaySound(pos, w.Sound)
	}
	for _, bp := range blocks {
		if t, ok := tx.Block(bp).(windToggler); ok {
			t.windBurstToggle(bp, tx, w.Source)
		}
	}
}

// stepOutOfBlock is the start of Explosion::explode: an explosion of an entity inside a solid block moves
// back along the entity's velocity, a block at a time, up to min(ceil(speed), 5) times, until it is out of
// it. It stays where it was if it does not get out.
func (w WindBurst) stepOutOfBlock(tx *world.Tx, pos mgl64.Vec3) mgl64.Vec3 {
	if w.Source == nil || !windSolidAt(tx, pos) {
		return pos
	}
	speed := w.SourceVelocity.Len()
	steps := min(int(math.Ceil(speed)), 5)
	if steps <= 0 {
		return pos
	}
	var dir mgl64.Vec3
	if speed >= 0.0001 {
		dir = w.SourceVelocity.Mul(1 / speed)
	}
	p := pos
	for i := 0; i < steps; i++ {
		p = p.Sub(dir)
		if !windSolidAt(tx, p) {
			return p
		}
	}
	return pos
}

// windSolidAt reports whether the block at p is solid (BDS Block::isSolid; here: a full cube).
func windSolidAt(tx *world.Tx, p mgl64.Vec3) bool {
	_, ok := tx.Block(cube.PosFromVec3(p)).Model().(model.Solid)
	return ok
}

// toggledBlocks casts the rays of Explosion::explode with the burst's flags: every ray starts with
// power*(0.7+rand*0.6), loses 0.225 a 0.3-block step and 0.09 in each block that is neither air nor water
// (block resistance ignored), and the blocks it reaches with strength left are toggled. Rays are not
// stopped by unbreakable blocks.
func (w WindBurst) toggledBlocks(tx *world.Tx, pos mgl64.Vec3, r *rand.Rand) []cube.Pos {
	var (
		seen   = make(map[cube.Pos]struct{}, 16)
		blocks = make([]cube.Pos, 0, 16)
	)
	for _, ray := range rays {
		p := pos
		for force := w.Power * (0.7 + r.Float64()*0.6); force > 0; force -= 0.225 {
			bp := cube.PosFromVec3(p)
			switch tx.Block(bp).(type) {
			case Air, Water:
			default:
				force -= 0.09
				if _, ok := seen[bp]; force > 0 && !ok {
					seen[bp] = struct{}{}
					blocks = append(blocks, bp)
				}
			}
			p = p.Add(ray)
		}
	}
	return blocks
}

// push is the entity part of Explosion::explode for a wind burst. Distance is measured from the centre to
// the entity's position (its eye x/z, the bottom of its box), less half its width unless it is a player,
// relative to twice the power. The push is (1 - distance) x seen percent x knockback scaling along the line
// from the centre to the entity's eyes, less its knockback resistance (KnockbackRules::getScaledKnockbackForce;
// none at all at resistance 1 or more). Spectators are left alone.
func (w WindBurst) push(tx *world.Tx, centre mgl64.Vec3, e world.Entity) {
	if g, ok := e.(interface{ GameMode() world.GameMode }); ok && g.GameMode() == world.GameModeSpectator {
		return
	}
	eye, res := windEntityInfo(e)
	feet := e.Position()
	dist := mgl64.Vec3{eye[0], feet[1], eye[2]}.Sub(centre).Len()
	player := e.H().Type().EncodeEntity() == "minecraft:player"
	if !player {
		dist = math.Max(dist-e.H().Type().BBox(e).Width()/2, 0)
	}
	ratio := dist / (w.Power * 2)
	if ratio > 1 {
		return
	}
	impact := (1 - ratio) * ExplosionConfig{}.exposure(tx, centre, e, nil) * w.KnockbackScaling
	dir := eye.Sub(centre)
	if dir.Len() < 0.0001 || impact == 0 {
		return
	}
	push := dir.Normalize().Mul(impact)
	if l, ok := e.(WindBurstLauncher); ok {
		l.WindBurstLaunch(push, w.NegatesFallDamage)
		return
	}
	if res >= 1 {
		return
	}
	if v, ok := e.(interface {
		Velocity() mgl64.Vec3
		SetVelocity(mgl64.Vec3)
	}); ok {
		v.SetVelocity(v.Velocity().Add(push.Mul(math.Max(1-res, 0))))
	}
}

// windEntityInfo returns the eye position and knockback resistance of e.
func windEntityInfo(e world.Entity) (mgl64.Vec3, float64) {
	if WindBurstEntityInfo != nil {
		if eye, res, ok := WindBurstEntityInfo(e); ok {
			return eye, res
		}
	}
	if eyed, ok := e.(interface{ EyeHeight() float64 }); ok {
		return e.Position().Add(mgl64.Vec3{0, eyed.EyeHeight()}), 0
	}
	return e.Position().Add(mgl64.Vec3{0, e.H().Type().BBox(e).Height() * 0.85}), 0
}

// finiteVec reports whether every component of v is a finite number.
func finiteVec(v mgl64.Vec3) bool {
	for _, c := range v {
		if math.IsNaN(c) || math.IsInf(c, 0) {
			return false
		}
	}
	return true
}

// The blocks a wind burst toggles, as BDS 1.21.3 does in <Block>::_onHitByActivatingAttack: doors (the
// lower half toggles the door; not iron) and trapdoors (not iron) and fence gates while unpowered, levers,
// buttons that are not pressed, bells, and lit candles, which go out.

func (d WoodDoor) windBurstToggle(pos cube.Pos, tx *world.Tx, src world.Entity) {
	if d.Top || tx.RedstonePower(pos) > 0 {
		return
	}
	d.Open = !d.Open
	tx.SetBlock(pos, d, openerSetOpts())
	if other, ok := tx.Block(pos.Side(cube.FaceUp)).(WoodDoor); ok {
		other.Open = d.Open
		tx.SetBlock(pos.Side(cube.FaceUp), other, openerSetOpts())
	}
	windDoorSound(pos, tx, d.Open, d, src)
}

func (d CopperDoor) windBurstToggle(pos cube.Pos, tx *world.Tx, src world.Entity) {
	if d.Top || tx.RedstonePower(pos) > 0 {
		return
	}
	d.Open = !d.Open
	tx.SetBlock(pos, d, openerSetOpts())
	if other, ok := tx.Block(pos.Side(cube.FaceUp)).(CopperDoor); ok {
		other.Open = d.Open
		tx.SetBlock(pos.Side(cube.FaceUp), other, openerSetOpts())
	}
	windDoorSound(pos, tx, d.Open, d, src)
}

func windDoorSound(pos cube.Pos, tx *world.Tx, open bool, b world.Block, src world.Entity) {
	if open {
		tx.PlaySound(pos.Vec3Centre(), sound.DoorOpen{Block: b})
	} else {
		tx.PlaySound(pos.Vec3Centre(), sound.DoorClose{Block: b})
	}
	postToggle(tx, pos, open, src)
}

func (t WoodTrapdoor) windBurstToggle(pos cube.Pos, tx *world.Tx, src world.Entity) {
	if tx.RedstonePower(pos) > 0 {
		return
	}
	t.Open = !t.Open
	tx.SetBlock(pos, t, openerSetOpts())
	windTrapdoorSound(pos, tx, t.Open, t, src)
}

func (t CopperTrapdoor) windBurstToggle(pos cube.Pos, tx *world.Tx, src world.Entity) {
	if tx.RedstonePower(pos) > 0 {
		return
	}
	t.Open = !t.Open
	tx.SetBlock(pos, t, openerSetOpts())
	windTrapdoorSound(pos, tx, t.Open, t, src)
}

func windTrapdoorSound(pos cube.Pos, tx *world.Tx, open bool, b world.Block, src world.Entity) {
	if open {
		tx.PlaySound(pos.Vec3Centre(), sound.TrapdoorOpen{Block: b})
	} else {
		tx.PlaySound(pos.Vec3Centre(), sound.TrapdoorClose{Block: b})
	}
	postToggle(tx, pos, open, src)
}

// windBurstToggle opens or closes the gate. Without a player (FenceGateBlock::_setOpen with no actor) it
// does not turn around to open away from anyone.
func (f WoodFenceGate) windBurstToggle(pos cube.Pos, tx *world.Tx, src world.Entity) {
	if tx.RedstonePower(pos) > 0 {
		return
	}
	f.Open = !f.Open
	tx.SetBlock(pos, f, openerSetOpts())
	if f.Open {
		tx.PlaySound(pos.Vec3Centre(), sound.FenceGateOpen{Block: f})
	} else {
		tx.PlaySound(pos.Vec3Centre(), sound.FenceGateClose{Block: f})
	}
	postToggle(tx, pos, f.Open, src)
}

// windBurstToggle flips the lever (LeverBlock::toggle with no player).
func (l Lever) windBurstToggle(pos cube.Pos, tx *world.Tx, src world.Entity) {
	l.Powered = !l.Powered
	tx.SetBlock(pos, l, nil)
	if l.Powered {
		tx.PlaySound(pos.Vec3Centre(), sound.PowerOn{})
	} else {
		tx.PlaySound(pos.Vec3Centre(), sound.PowerOff{})
	}
	postActivate(tx, pos, l.Powered, src)
}

// windBurstToggle puts lit candles out (AbstractCandleBlock::_extinguish: extinguish.candle, block_change).
func (c Candle) windBurstToggle(pos cube.Pos, tx *world.Tx, src world.Entity) {
	if !c.Lit {
		return
	}
	c.Lit = false
	tx.SetBlock(pos, c, nil)
	tx.PlaySound(pos.Vec3Centre(), sound.CandleExtinguish{})
	postBlockEvent(tx, gameevent.BlockChange, pos, src)
}
