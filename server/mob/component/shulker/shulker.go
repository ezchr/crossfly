//go:build crossfly_full

// Package shulker is minecraft:peek and the hard-coded parts of BDS's
// Shulker class: the shulker sits attached to a block face, opens and closes
// its shell (20 armour and immune to projectiles while closed), and only
// ever moves by teleporting: when hurt by fire or lava, sometimes below half
// health, when its block goes or it cannot open, and when a shulker bullet
// hits it (which may also spawn a second shulker). It is never knocked back,
// pushed or pulled down by gravity while attached.
//
// The Bedrock client is shown the peek, attach face and attach position
// actor data; the Java session maps them to Shulker's attach face and peek
// (javasession/mobdata_shulker.go).
package shulker

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// ID is the shulker's identifier.
const ID = "minecraft:shulker"

// bulletID is the projectile whose hits make a shulker duplicate.
const bulletID = "minecraft:shulker_bullet"

// Peek amounts (actor data PeekID, 0..100): closed, a random peek, open at a
// target.
const (
	peekClosed = 0
	peekRandom = 30
	peekTarget = 100
)

// closedArmour is Shulker::getArmorValue with the shell closed.
const closedArmour = 20

func init() {
	mob.RegisterComponent("minecraft:peek", newPeek)
	// Shulker::postNormalTick, after travel (BDS skips travel for shulkers:
	// PermanentSkipMobTravelFlag, ShulkerPostAiStepSystem).
	mob.RegisterSystem(mob.System{Name: "shulker/still", Stage: mob.StageControls, Order: 900, Tick: still})
	mob.RegisterSystem(mob.System{Name: "shulker/attach", Stage: mob.StageTravel, Order: 900, Tick: postNormalTick})
}

// Peek is minecraft:peek (BDS PeekComponent, PeekSystem) with the state of
// BDS's Shulker class, which is the only mob it works on ("Peek component
// not supported on custom entities").
type Peek struct {
	onOpen, onClose, onTargetOpen sense.Trigger

	// tickPeekComponent state.
	hadTarget bool
	duration  int

	peek int32 // PeekID, 0..100

	// The attachment: face is the side of attachPos the shulker sits on
	// (cube.Face numbering, as Bedrock's: 0 down = on the floor).
	face      cube.Face
	attachPos cube.Pos
	attached  bool
	// placed is where the shulker was last put, before is where it was
	// before travel: a move further than a block between them is a
	// teleport from outside (a command), which re-attaches it there
	// (postSetPosSetShulkerAttachPosAndPeekAmount).
	placed, before mgl64.Vec3
	haveBefore     bool

	// amount is ShulkerPeekAmountComponent: the shell's opening, 0..1,
	// following peek by 0.05 a tick.
	amount float64
}

func newPeek(_ string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		OnOpen       json.RawMessage `json:"on_open"`
		OnClose      json.RawMessage `json:"on_close"`
		OnTargetOpen json.RawMessage `json:"on_target_open"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("minecraft:peek: %w", err)
	}
	p := &Peek{}
	var err error
	if p.onOpen, err = sense.ParseTrigger(v.OnOpen); err != nil {
		return nil, err
	}
	if p.onClose, err = sense.ParseTrigger(v.OnClose); err != nil {
		return nil, err
	}
	if p.onTargetOpen, err = sense.ParseTrigger(v.OnTargetOpen); err != nil {
		return nil, err
	}
	return p, nil
}

// Attach ...
func (p *Peek) Attach(m *mob.Mob) {
	// Knockback never moves a shulker (its travel is skipped and
	// ShulkerPostAiStepSystem zeroes its motion): no velocity, and no
	// knockback for Java clients to predict.
	m.Attributes().SetBase(mob.AttrKnockbackResistance, 1)
	if m.Identifier() == ID {
		// The shulker has no collision_box: BDS sizes it in code, a block
		// (Shulker::computeShulkerBounds with the shell shut).
		m.UpdateMeta(func(meta *mob.Meta) { meta.Width, meta.Height = 1, 1 })
	}
	p.show(m)
}

// Detach ...
func (*Peek) Detach(*mob.Mob) {}

// Of returns the shulker state of m.
func Of(m *mob.Mob) (*Peek, bool) { return mob.ComponentOf[*Peek](m) }

// PeekAmount is the peek actor data (0 closed .. 100 open).
func (p *Peek) PeekAmount() int32 { return p.peek }

// AttachFace is the face of the attach position the shulker sits on.
func (p *Peek) AttachFace() cube.Face { return p.face }

// AttachPos is the block the shulker is in.
func (p *Peek) AttachPos() cube.Pos { return p.attachPos }

// Closed reports whether the shell is shut.
func (p *Peek) Closed() bool { return p.peek == peekClosed }

// setPeek is Shulker::_setPeekAmount.
func (p *Peek) setPeek(m *mob.Mob, v int32) {
	p.peek = v
	m.SetActorData(protocol.EntityDataKeyPeekID, v)
}

// show sends the attachment and peek actor data.
func (p *Peek) show(m *mob.Mob) {
	m.SetActorData(protocol.EntityDataKeyPeekID, p.peek)
	m.SetActorData(protocol.EntityDataKeyAttachFace, byte(p.face))
	if p.attached {
		m.SetActorData(protocol.EntityDataKeyAttached, int16(1))
		m.SetActorData(protocol.EntityDataKeyAttachedPosition, protocol.BlockPos{int32(p.attachPos[0]), int32(p.attachPos[1]), int32(p.attachPos[2])})
	}
}

// setAttach is Shulker::setShulkerAttachPos.
func (p *Peek) setAttach(m *mob.Mob, pos cube.Pos) {
	p.attachPos, p.attached = pos, true
	p.show(m)
}

// setFace is Shulker::_setAttachFace.
func (p *Peek) setFace(m *mob.Mob, f cube.Face) {
	p.face = f
	m.SetActorData(protocol.EntityDataKeyAttachFace, byte(f))
}

// Tick is tickPeekComponent: without a target the shell peeks open now and
// then (1 in 40 a tick, for 1..3 s); a new target opens it fully; it shuts
// again when the target is gone and the peek ran out.
func (p *Peek) Tick(m *mob.Mob) {
	_, hasTarget := m.Target()
	ctx := m.Ctx()
	if t, ok := m.Target(); ok {
		ctx.Target, ctx.Other = t, t
	}
	if !hasTarget && p.duration <= 0 && m.Rand().IntN(40) == 0 {
		p.duration = 20 * (m.Rand().IntN(3) + 1)
		p.setPeek(m, peekRandom)
		p.onOpen.Run(m, ctx)
		m.PlaySound("shulker.open")
	}
	if p.duration > 0 {
		p.duration--
	}
	if p.duration == 0 && !hasTarget {
		if p.peek != peekClosed {
			p.setPeek(m, peekClosed)
			p.onClose.Run(m, ctx)
			m.PlaySound("shulker.close")
		}
		p.hadTarget = false
		return
	}
	if hasTarget && !p.hadTarget {
		p.setPeek(m, peekTarget)
		p.onTargetOpen.Run(m, ctx)
		m.PlaySound("shulker.open")
	}
	p.hadTarget = hasTarget
}

// ModifyDamage is Shulker::_hurt and Shulker::isInvulnerableTo before the
// damage applies: fire and lava make it teleport instead of burning; shut,
// it ignores projectiles and has 20 armour.
func (p *Peek) ModifyDamage(m *mob.Mob, d *mob.Damage) {
	if m.Identifier() != ID {
		return
	}
	switch sense.DamageCause(d.Source) {
	case "fire", "lava":
		p.tryTeleport(m)
		d.Cancel = true
		return
	case "projectile":
		if p.Closed() {
			d.Cancel = true
			return
		}
	}
	if p.Closed() && d.Source.ReducedByArmour() {
		d.Amount = armourReduced(d.Amount, closedArmour)
	}
}

// armourReduced is Mob::getDamageAfterArmorReduction with no armour
// toughness: the share min(20, max(armour + damage/2, armour/5)) x 4% is
// taken off (BDS 1.21.3 adds the damage term).
func armourReduced(dmg float64, armour float64) float64 {
	f := math.Min(20, math.Max(armour+dmg/2, armour*0.2))
	return dmg * (1 - f*0.04)
}

// OnHurt is the rest of Shulker::_hurt: below half health it teleports 1 in
// 4 times, and a shulker bullet makes it try to duplicate.
func (p *Peek) OnHurt(m *mob.Mob, _ float64, src world.DamageSource) {
	if m.Identifier() != ID || m.Dead() {
		return
	}
	if m.Health() < m.MaxHealth()*0.5 && m.Rand().IntN(4) == 0 {
		p.tryTeleport(m)
	}
	if s, ok := src.(entity.ProjectileDamageSource); ok && s.Projectile != nil && s.Projectile.H().Type().EncodeEntity() == bulletID {
		p.trySpawnShulker(m)
	}
}

// EncodeNBT saves the attachment and peek (BDS AttachPos, AttachFace, Peek).
func (p *Peek) EncodeNBT() map[string]any {
	out := map[string]any{"AttachFace": byte(p.face), "Peek": byte(p.peek)}
	if p.attached {
		out["AttachPos"] = []int32{int32(p.attachPos[0]), int32(p.attachPos[1]), int32(p.attachPos[2])}
	}
	return out
}

// DecodeNBT ...
func (p *Peek) DecodeNBT(m *mob.Mob, d map[string]any) {
	if f, ok := d["AttachFace"].(byte); ok && f < 6 {
		p.face = cube.Face(f)
	}
	if v, ok := d["Peek"].(byte); ok {
		p.peek = int32(v)
		p.amount = float64(v) / 100
	}
	if pos, ok := d["AttachPos"].([]int32); ok && len(pos) == 3 {
		p.attachPos, p.attached = cube.Pos{int(pos[0]), int(pos[1]), int(pos[2])}, true
		p.placed = attachCentre(p.attachPos)
	}
	p.show(m)
}

// attachCentre is where a shulker attached in pos stands.
func attachCentre(pos cube.Pos) mgl64.Vec3 {
	return mgl64.Vec3{float64(pos[0]) + 0.5, float64(pos[1]), float64(pos[2]) + 0.5}
}

// postNormalTick is Shulker::postNormalTick plus ShulkerPostAiStepSystem:
// keep the attachment valid (re-attach to another face, else teleport),
// follow the peek, and stand still in the attach position.
func postNormalTick(m *mob.Mob) {
	if m.Identifier() != ID || m.Dead() {
		return
	}
	p, ok := Of(m)
	if !ok {
		return
	}
	if m.Riding() {
		if p.face != cube.FaceDown {
			p.setFace(m, cube.FaceDown)
		}
		p.amountTick()
		return
	}
	// Travel still pulled it down a little: what counts is where it was
	// before.
	pos := p.before
	if !p.haveBefore {
		pos = m.Position() // the first tick: components attached after still ran
	}
	p.haveBefore = false
	if !p.attached || pos.Sub(p.placed).Len() > 1 {
		// Not attached yet, or put elsewhere by a command: attach there,
		// shut.
		if p.attached {
			p.setPeek(m, peekClosed)
		}
		p.setAttach(m, cube.PosFromVec3(pos.Add(mgl64.Vec3{0, 1e-4, 0})))
	}
	tx := m.Tx()
	valid := validAttach(tx, p.attachPos.Side(p.face), p.face)
	canOpen := canOpenLidAt(tx, m, p.attachPos, p.face)
	teleport := false
	if !valid || !canOpen {
		teleport = !p.attachToNeighbour(m, p.attachPos)
	}
	if occupiedByOtherShulker(tx, m, p.attachPos) || teleport || !canBreathe(m) {
		p.tryTeleport(m)
	}
	p.amountTick()
	p.place(m)
}

// still drops any motion before travel (ShulkerPostAiStepSystem zeroes it;
// BDS never runs the shulker's travel), so pushes from outside the tick
// (explosions) do not move it.
func still(m *mob.Mob) {
	if m.Identifier() != ID || m.Riding() {
		return
	}
	m.SetVelocity(mgl64.Vec3{})
	if p, ok := Of(m); ok {
		p.before, p.haveBefore = m.Position(), true
	}
}

// place puts the shulker in its attach position, still, facing south (its
// body and yaw are zeroed; the head turns on its own).
func (p *Peek) place(m *mob.Mob) {
	c := attachCentre(p.attachPos)
	m.SetPosition(c)
	m.SetVelocity(mgl64.Vec3{})
	m.ResetFallDistance()
	m.SetYaw(0)
	m.SetBodyYaw(0)
	p.placed = c
}

// amountTick is Shulker::_peekAmountTick.
func (p *Peek) amountTick() {
	want := float64(p.peek) * 0.01
	switch {
	case p.amount > want:
		p.amount = math.Max(p.amount-0.05, want)
	case p.amount < want:
		p.amount = math.Min(p.amount+0.05, want)
	}
}

// solid is BlockSource::isSolidBlockingBlock for the attachment checks.
func solid(tx *world.Tx, pos cube.Pos) bool {
	b := tx.Block(pos)
	if _, air := b.(block.Air); air {
		return false
	}
	return b.Model().FaceSolid(pos, cube.FaceUp, tx) && b.Model().FaceSolid(pos, cube.FaceDown, tx)
}

// isPiston reports whether the block at pos is a piston (shulkers cannot sit
// on top of one).
func isPiston(tx *world.Tx, pos cube.Pos) bool {
	n, _ := tx.Block(pos).EncodeBlock()
	return n == "minecraft:piston" || n == "minecraft:sticky_piston"
}

// validAttach is Shulker::_isValidAttach: the block to sit on is solid, and
// not a piston under a shulker on the floor.
func validAttach(tx *world.Tx, pos cube.Pos, face cube.Face) bool {
	if !solid(tx, pos) {
		return false
	}
	return face != cube.FaceDown || !isPiston(tx, pos)
}

// canOpenLidAt is Shulker::_canOpenLidAt: the block the lid opens into is
// free.
func canOpenLidAt(tx *world.Tx, m *mob.Mob, pos cube.Pos, face cube.Face) bool {
	lid := pos.Side(face.Opposite())
	return !solid(tx, lid) && !occupiedByOtherShulker(tx, m, lid)
}

// occupiedByOtherShulker is Shulker::_isPosOccupiedByOtherShulker.
func occupiedByOtherShulker(tx *world.Tx, m *mob.Mob, pos cube.Pos) bool {
	box := cube.Box(float64(pos[0])-1, float64(pos[1])-1, float64(pos[2])-1, float64(pos[0])+2, float64(pos[1])+2, float64(pos[2])+2)
	for e := range tx.EntitiesWithin(box) {
		if e.H() == m.H() || e.H().Type().EncodeEntity() != ID {
			continue
		}
		if cube.PosFromVec3(e.Position().Add(mgl64.Vec3{0, 1e-4, 0})) == pos {
			return true
		}
	}
	return false
}

// attachToNeighbour is Shulker::_tryAttachingToNeighbouringFaces: the first
// face (down, up, north, south, west, east) with a block to sit on and room
// for the lid.
func (p *Peek) attachToNeighbour(m *mob.Mob, pos cube.Pos) bool {
	tx := m.Tx()
	for f := cube.FaceDown; f <= cube.FaceEast; f++ {
		if !validAttach(tx, pos.Side(f), f) {
			continue
		}
		if !canOpenLidAt(tx, m, pos, f) {
			continue
		}
		p.setFace(m, f)
		return true
	}
	return false
}

// canBreathe is BreathableComponent::canBreathe for the shulker's
// minecraft:breathable (air only): not in water, lava or a solid block.
func canBreathe(m *mob.Mob) bool {
	if !m.HasComponent("minecraft:breathable") {
		return true
	}
	tx := m.Tx()
	eye := m.EyePosition()
	pos := cube.PosFromVec3(eye)
	if l, ok := tx.Liquid(pos); ok {
		top := float64(pos[1]) + 1 - (1-float64(l.LiquidDepth())/8)*0.9
		if eye[1] < top {
			return false
		}
	}
	return !solid(tx, pos)
}

// tryTeleport is Shulker::_tryTeleportSomewhere: 5 tries at a random air
// block within 8 on each axis that another shulker is not in and that has a
// face to attach to. It reports whether the shulker moved.
func (p *Peek) tryTeleport(m *mob.Mob) bool {
	if m.Dead() {
		return false
	}
	tx := m.Tx()
	base := cube.PosFromVec3(m.Position().Add(mgl64.Vec3{0, 1e-4, 0}))
	r := m.Rand()
	for range 5 {
		dest := base.Add(cube.Pos{r.IntN(17) - 8, r.IntN(17) - 8, r.IntN(17) - 8})
		if _, air := tx.Block(dest).(block.Air); !air || dest[1] <= tx.Range()[0] {
			continue
		}
		if occupiedByOtherShulker(tx, m, dest) || !p.attachToNeighbour(m, dest) {
			continue
		}
		m.PlaySound("teleport")
		p.setAttach(m, dest)
		p.setPeek(m, peekClosed)
		m.ResetTarget()
		c := attachCentre(dest)
		m.Teleport(c)
		p.placed = c
		return true
	}
	return false
}

// trySpawnShulker is Shulker::_trySpawnShulker: an open shulker hit by a
// bullet that manages to teleport leaves a new shulker where it was, less
// likely the more shulkers are around (each other one within 4 boxes takes
// 20% off).
func (p *Peek) trySpawnShulker(m *mob.Mob) {
	if p.Closed() {
		return
	}
	old := m.Position()
	box := m.BBox()
	if !p.tryTeleport(m) {
		return
	}
	tx := m.Tx()
	c := box.Translate(old)
	size := mgl64.Vec3{box.Width(), box.Height(), box.Length()}.Mul(4)
	mid := c.Min().Add(c.Max()).Mul(0.5)
	area := cube.Box(mid[0]-size[0], mid[1]-size[1], mid[2]-size[2], mid[0]+size[0], mid[1]+size[1], mid[2]+size[2])
	n := 0
	for e := range tx.EntitiesWithin(area) {
		if e.H().Type().EncodeEntity() == ID {
			n++
		}
	}
	if m.Rand().Float64() < float64(n-1)*0.2 {
		return
	}
	s, err := mob.Spawn(tx, ID, old, mob.SpawnOptions{})
	if err == nil && m.Persistent() {
		s.SetPersistent(true)
	}
}
