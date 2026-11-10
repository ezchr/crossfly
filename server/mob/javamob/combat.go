//go:build crossfly_full

package javamob

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// ModifyDamage implements mob.DamageModifier: the behaviour's DamageFilter (the start of its hurtServer).
func (s *state) ModifyDamage(bm *mob.Mob, d *mob.Damage) {
	if f, ok := s.b.(DamageFilter); ok {
		if m, ok := wrap(bm); ok {
			f.FilterDamage(m, d)
		}
	}
}

// OnHurt implements mob.HurtListener: LivingEntity.hurtServer's bookkeeping (hurtTime, the attacker, noActionTime)
// and the behaviour's HurtListener.
func (s *state) OnHurt(bm *mob.Mob, dealt float64, src world.DamageSource) {
	m, ok := wrap(bm)
	if !ok {
		return
	}
	s.hurtTime = 10
	s.noActionTime = 0
	s.lastHurt = dealt
	if a := attacker(src); a != nil {
		if _, living := a.(entity.Living); living {
			s.lastHurtByMobTime = s.tickCount
		}
	}
	if l, ok := s.b.(HurtListener); ok {
		l.OnHurt(m, dealt, src)
	}
}

// OnDeath implements mob.DeathListener: LivingEntity.die and dropAllDeathLoot.
func (s *state) OnDeath(bm *mob.Mob, src world.DamageSource) {
	m, ok := wrap(bm)
	if !ok {
		return
	}
	s.goals.StopAll(m)
	s.targets.StopAll(m)
	s.nav.Stop()
	if l, ok := s.b.(DeathListener); ok {
		l.OnDeath(m, src)
		if m.gone() {
			return
		}
	}
	m.dropAllDeathLoot(src)
}

// OnTargetChanged implements mob.TargetListener: the behaviour's TargetListener.
func (s *state) OnTargetChanged(bm *mob.Mob, old, new world.Entity) {
	if l, ok := s.b.(TargetListener); ok {
		if m, ok := wrap(bm); ok {
			l.OnTargetChanged(m, old, new)
		}
	}
}

// Interact implements mob.Interactor: Mob.mobInteract.
func (s *state) Interact(bm *mob.Mob, user item.User, held item.Stack, _ mgl64.Vec3) (item.Stack, bool) {
	i, ok := s.b.(Interactor)
	if !ok {
		return held, false
	}
	m, ok := wrap(bm)
	if !ok {
		return held, false
	}
	return i.MobInteract(m, user, held)
}

// attacker is the entity responsible for src (DamageSource.getEntity).
func attacker(src world.DamageSource) world.Entity {
	switch s := src.(type) {
	case entity.AttackDamageSource:
		return s.Attacker
	case entity.ProjectileDamageSource:
		if s.Owner != nil {
			return s.Owner
		}
		return s.Projectile
	}
	return nil
}

// SourceEntity returns the entity responsible for a damage source (DamageSource.getEntity), or nil.
func SourceEntity(src world.DamageSource) world.Entity { return attacker(src) }

// knockBackFrom is the KnockBack hook: Java's LivingEntity.knockback away from src. Dragonfly's attacks pass the
// Bedrock base (0.45, 0.3608); it is Java's 0.4 from hurtServer, and whatever the attack adds above it (the
// knockback enchantment) is the extra causeExtraKnockback push.
func (m *Mob) knockBackFrom(src mgl64.Vec3, force, _ float64) {
	if m.Dead() {
		return
	}
	pos := m.Position()
	m.Knockback(0.4, src[0]-pos[0], src[2]-pos[2])
	if extra := force - 0.45; extra > 1e-6 {
		m.Knockback(extra, src[0]-pos[0], src[2]-pos[2])
	}
}

// Knockback is LivingEntity.knockback(power, xd, zd): pushed away from the direction (xd, zd).
func (m *Mob) Knockback(power, xd, zd float64) {
	power *= 1 - m.Attr(AttrKnockbackResistance)
	if power <= 0 {
		return
	}
	m.SyncMovement()
	v := m.Velocity()
	for xd*xd+zd*zd < float64(float32(1e-5)) {
		xd = (m.Rand().Float64() - m.Rand().Float64()) * 0.01
		zd = (m.Rand().Float64() - m.Rand().Float64()) * 0.01
	}
	d := mgl64.Vec3{xd, 0, zd}.Normalize().Mul(power)
	vy := v[1]
	if m.s.onGround {
		vy = math.Min(0.4, v[1]/2+power)
	}
	m.SetVelocity(mgl64.Vec3{v[0]/2 - d[0], vy, v[2]/2 - d[2]})
}

// DoHurtTarget is Mob.doHurtTarget: hit the target with the attack damage attribute (the behaviour's Attacker
// replaces it).
func (m *Mob) DoHurtTarget(target world.Entity) bool {
	if a, ok := m.s.b.(Attacker); ok {
		return a.DoHurtTarget(m, target)
	}
	return m.BaseDoHurtTarget(target)
}

// BaseDoHurtTarget is Mob.doHurtTarget itself, for behaviours that add to it.
func (m *Mob) BaseDoHurtTarget(target world.Entity) bool {
	l, ok := target.(entity.Living)
	if !ok {
		return false
	}
	dmg := float64(float32(m.Attr(AttrAttackDamage)))
	_, res := l.Hurt(dmg, entity.AttackDamageSource{Attacker: m})
	if !res.Accepted() {
		return false
	}
	if kb := float32(m.Attr(AttrAttackKnockback)) / 2; kb > 0 {
		rad := float32(m.YRot()) * math.Pi / 180
		if jm, ok := Of(target); ok {
			jm.Knockback(float64(kb), float64(mthSin(rad)), float64(-mthCos(rad)))
		} else {
			l.KnockBack(m.Position(), float64(kb), float64(kb))
		}
		v := m.Velocity()
		m.SetVelocity(mgl64.Vec3{v[0] * 0.6, v[1], v[2] * 0.6})
	}
	m.SetLastHurtMob(target)
	return true
}

// IsWithinMeleeAttackRange is Mob.isWithinMeleeAttackRange: the box grown by sqrt(2.04)-0.6 sideways meets the
// target's box.
func (m *Mob) IsWithinMeleeAttackRange(target world.Entity) bool {
	reach := math.Sqrt(float64(float32(2.04))) - float64(float32(0.6))
	bb := m.BBox().GrowVec3(mgl64.Vec3{reach, 0, reach})
	return overlaps(bb, boxOf(target))
}

// boxOf is the bounding box of an entity in the world.
func boxOf(e world.Entity) cube.BBox {
	return e.H().Type().BBox(e).Translate(e.Position())
}

// killedByPlayer reports whether a player hurt the mob within the last 100 ticks (lastHurtByPlayerMemoryTime).
func (m *Mob) killedByPlayer() bool {
	t := m.LastHurtByPlayerTick()
	return t > 0 && m.Ticks()-t < 100
}

// dropAllDeathLoot is LivingEntity.dropAllDeathLoot.
func (m *Mob) dropAllDeathLoot(src world.DamageSource) {
	byPlayer := m.killedByPlayer()
	if m.s.lootTable != "" {
		for _, st := range RollLoot(m.s.lootTable, m.lootContext(src, byPlayer)) {
			m.SpawnAtLocation(st)
		}
	}
	if d, ok := m.s.b.(DeathLooter); ok {
		d.DropCustomDeathLoot(m, src, byPlayer)
	}
	m.dropEquipment(byPlayer)
	if byPlayer && !m.Baby() {
		if xp := m.xpReward(); xp > 0 {
			for _, h := range entity.NewExperienceOrbs(m.Position(), xp) {
				m.Tx().AddEntity(h)
			}
		}
	}
}

// xpReward is Mob.getBaseExperienceReward: the type's reward plus 1-3 per worn piece with a normal drop chance.
func (m *Mob) xpReward() int {
	xp := m.s.t.XPReward
	if x, ok := m.s.b.(XPModifier); ok {
		xp = x.XPReward(m, xp)
	}
	if xp <= 0 {
		return xp
	}
	for slot := SlotFeet; slot <= SlotHead; slot++ {
		if !m.ItemBySlot(slot).Empty() && m.s.dropChances[slot] <= 1 {
			xp += 1 + m.Rand().IntN(3)
		}
	}
	for _, slot := range []Slot{SlotMainHand, SlotOffHand} {
		if !m.ItemBySlot(slot).Empty() && m.s.dropChances[slot] <= 1 {
			xp += 1 + m.Rand().IntN(3)
		}
	}
	return xp
}

// dropEquipment is Mob.dropCustomDeathLoot's equipment part: each slot drops with its drop chance (killed by a
// player, or a guaranteed drop), damaged unless guaranteed.
func (m *Mob) dropEquipment(byPlayer bool) {
	for slot := Slot(0); slot < slotCount; slot++ {
		st := m.ItemBySlot(slot)
		chance := m.s.dropChances[slot]
		if st.Empty() || chance == 0 {
			continue
		}
		preserve := chance > 1
		if !(byPlayer || preserve) || !(m.Rand().Float32() < float32(chance)) {
			continue
		}
		if dur, ok := st.Item().(item.Durable); ok && !preserve {
			maxDmg := dur.DurabilityInfo().MaxDurability
			dmg := maxDmg - m.Rand().IntN(1+m.Rand().IntN(max(maxDmg-3, 1)))
			st = st.WithDurability(maxDmg - dmg)
		}
		m.SpawnAtLocation(st)
		m.SetItemSlot(slot, item.Stack{})
	}
}

// SpawnAtLocation is Entity.spawnAtLocation: the item dropped at the mob's feet (offset 0) with a 10 tick pickup
// delay and a random push.
func (m *Mob) SpawnAtLocation(st item.Stack) {
	if st.Empty() {
		return
	}
	r := m.Rand()
	vel := mgl64.Vec3{r.Float64()*0.2 - 0.1, 0.2, r.Float64()*0.2 - 0.1}
	opts := world.EntitySpawnOpts{Position: m.Position(), Velocity: vel}
	m.Tx().AddEntity(entity.NewItemPickupDelay(opts, st, time.Second/2))
}

// Slot is an EquipmentSlot.
type Slot uint8

const (
	SlotMainHand Slot = iota
	SlotOffHand
	SlotFeet
	SlotLegs
	SlotChest
	SlotHead
	slotCount
)

// dropChances are DropChances: the chance per slot an item drops on death; above 1 it is a guaranteed, undamaged
// drop (setGuaranteedDrop: 2).
type dropChances [slotCount]float64

func defaultDropChances() dropChances {
	var d dropChances
	for i := range d {
		d[i] = float64(float32(0.085))
	}
	return d
}

// SetDropChance is Mob.setDropChance.
func (m *Mob) SetDropChance(slot Slot, chance float64) { m.s.dropChances[slot] = chance }

// SetGuaranteedDrop is Mob.setGuaranteedDrop.
func (m *Mob) SetGuaranteedDrop(slot Slot) { m.s.dropChances[slot] = 2 }

// ItemBySlot is getItemBySlot.
func (m *Mob) ItemBySlot(slot Slot) item.Stack {
	switch slot {
	case SlotMainHand:
		h, _ := m.HeldItems()
		return h
	case SlotOffHand:
		_, o := m.HeldItems()
		return o
	}
	a := m.Armour()
	switch slot {
	case SlotFeet:
		return a.Boots()
	case SlotLegs:
		return a.Leggings()
	case SlotChest:
		return a.Chestplate()
	}
	return a.Helmet()
}

// SetItemSlot is setItemSlot.
func (m *Mob) SetItemSlot(slot Slot, st item.Stack) {
	switch slot {
	case SlotMainHand:
		_, o := m.HeldItems()
		m.SetHeldItems(st, o)
		return
	case SlotOffHand:
		h, _ := m.HeldItems()
		m.SetHeldItems(h, st)
		return
	}
	a := m.Armour()
	switch slot {
	case SlotFeet:
		a.SetBoots(st)
	case SlotLegs:
		a.SetLeggings(st)
	case SlotChest:
		a.SetChestplate(st)
	case SlotHead:
		a.SetHelmet(st)
	}
}

var _ = inventory.NewArmour

// damageHelmet damages the worn helmet by n (hurtAndBreak), breaking it at its durability.
func (m *Mob) damageHelmet(n int) {
	h := m.Armour().Helmet()
	if n <= 0 || h.Empty() {
		return
	}
	if _, ok := h.Item().(item.Durable); !ok {
		return
	}
	h = h.Damage(n)
	m.Armour().SetHelmet(h)
}

// validTarget is asValidTarget plus TargetFilter: creative and spectator players and the dead are no targets.
func (m *Mob) validTarget(e world.Entity) bool {
	if e == nil || e.H() == m.H() {
		return false
	}
	if l, ok := e.(entity.Living); ok && l.Dead() {
		return false
	}
	if p, ok := e.(*player.Player); ok {
		gm := p.GameMode()
		if !gm.AllowsTakingDamage() || !gm.Visible() {
			return false
		}
	}
	if f, ok := m.s.b.(TargetFilter); ok && !f.CanAttack(m, e) {
		return false
	}
	return true
}

// DifficultyInstance is Java's DifficultyInstance at a position: the local difficulty (0..6.75) and its special
// multiplier (0..1). Dragonfly does not track how long chunks were inhabited, so that part is 0.
type DifficultyInstance struct {
	Effective, Special float64
}

// Difficulty is the DifficultyInstance of the mob's world at the mob (Level.getCurrentDifficultyAt).
func (m *Mob) Difficulty() DifficultyInstance {
	tx := m.Tx()
	base := difficultyID(tx)
	if base == 0 {
		return DifficultyInstance{}
	}
	t := float64(tx.World().Time())
	scale := 0.75
	global := math.Max(0, math.Min(1, (t-72000)/1440000)) * 0.25
	scale += global
	moon := [8]float64{1, 0.75, 0.5, 0.25, 0, 0.25, 0.5, 0.75}[int(t/24000)%8]
	local := math.Max(0, math.Min(global, moon*0.25))
	if base == 1 {
		local *= 0.5
	}
	scale += local
	eff := float64(float32(float64(base) * scale))
	sp := 0.0
	switch {
	case eff < 2:
	case eff > 4:
		sp = 1
	default:
		sp = (eff - 2) / 2
	}
	return DifficultyInstance{Effective: eff, Special: sp}
}

// PopulateDefaultEquipmentSlots is Mob.populateDefaultEquipmentSlots: random armour, more often on harder
// difficulty.
func (m *Mob) PopulateDefaultEquipmentSlots(d DifficultyInstance) {
	r := m.Rand()
	if !(r.Float32() < float32(0.15*d.Special)) {
		return
	}
	armourType := r.IntN(3)
	for i := 1; i <= 3; i++ {
		if r.Float32() < 0.1087 {
			armourType++
		}
	}
	partial := float32(0.25)
	if m.DifficultyID() == 3 {
		partial = 0.1
	}
	first := true
	for _, slot := range []Slot{SlotHead, SlotChest, SlotLegs, SlotFeet} {
		if !first && r.Float32() < partial {
			break
		}
		first = false
		if m.ItemBySlot(slot).Empty() {
			if it, ok := EquipmentForSlot(slot, armourType); ok {
				m.SetItemSlot(slot, item.NewStack(it, 1))
			}
		}
	}
}

// EquipmentForSlot is Mob.getEquipmentForSlot: the armour piece of tier type (0 leather, 1 copper, 2 gold,
// 3 chain, 4 iron, 5 diamond).
func EquipmentForSlot(slot Slot, typ int) (world.Item, bool) {
	var tier item.ArmourTier
	switch typ {
	case 0:
		tier = item.ArmourTierLeather{}
	case 1:
		tier = item.ArmourTierCopper{}
	case 2:
		tier = item.ArmourTierGold{}
	case 3:
		tier = item.ArmourTierChain{}
	case 4:
		tier = item.ArmourTierIron{}
	case 5:
		tier = item.ArmourTierDiamond{}
	default:
		return nil, false
	}
	switch slot {
	case SlotHead:
		return item.Helmet{Tier: tier}, true
	case SlotChest:
		return item.Chestplate{Tier: tier}, true
	case SlotLegs:
		return item.Leggings{Tier: tier}, true
	case SlotFeet:
		return item.Boots{Tier: tier}, true
	}
	return nil, false
}
