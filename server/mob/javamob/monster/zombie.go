//go:build crossfly_full

// Package monster holds Java Edition's zombie, skeleton, creeper, spider and enderman for Java mob mode
// (net.minecraft.world.entity.monster, 26.3, with Paper's default config). Zombie and AbstractSkeleton are
// exported so that their subclasses in other packages (husk, drowned, zombified piglin, stray, wither skeleton,
// bogged) embed them.
package monster

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func init() {
	javamob.Register(javamob.Type{
		ID: "minecraft:zombie", Category: javamob.CategoryMonster, Width: 0.6, Height: 1.95, EyeHeight: 1.74,
		NotInPeaceful: true, BurnsInDaylight: true, Undead: true, XPReward: 5,
		Families:   []string{"zombie", "undead", "monster", "mob"},
		Attributes: ZombieAttributes,
		New:        func() javamob.Behaviour { return NewZombie() },
	})
}

// ZombieAttributes is Zombie.createAttributes.
func ZombieAttributes() map[javamob.Attribute]float64 {
	a := javamob.MonsterAttributes()
	a[javamob.AttrFollowRange] = 35
	a[javamob.AttrMovementSpeed] = float64(float32(0.23))
	a[javamob.AttrAttackDamage] = 3
	a[javamob.AttrArmor] = 2
	a[javamob.AttrSpawnReinforcements] = 0
	return a
}

// Zombie is Java's Zombie. Its subclasses embed it and change the exported fields (ConvertsTo, the goal hooks)
// before the mob is set up (in their Type.New).
type Zombie struct {
	// ConvertsTo is the type the zombie turns into after 30 seconds under water and 15 more shaking
	// (convertsToWhenDrowning; Zombie: drowned). Empty: it does not convert (convertsInWater false).
	ConvertsTo string
	// BehaviourGoals, if set, replaces addBehaviourGoals.
	BehaviourGoals func(z *Zombie, m *javamob.Mob)
	// ExtraEquipment, if set, replaces the zombie's weapon roll of populateDefaultEquipmentSlots.
	ExtraEquipment func(z *Zombie, m *javamob.Mob, d javamob.DifficultyInstance)

	canBreakDoors bool
	breakDoor     *goal.BreakDoor
	inWaterTime   int
	convertTime   int
	converting    bool
}

// NewZombie returns a zombie's behaviour.
func NewZombie() *Zombie { return &Zombie{ConvertsTo: "minecraft:drowned"} }

// RegisterGoals is Zombie.registerGoals.
func (z *Zombie) RegisterGoals(m *javamob.Mob) {
	z.breakDoor = goal.NewBreakDoor(func(d int) bool { return d == 3 })
	m.Goals().Add(8, goal.NewLookAtPlayer(8))
	m.Goals().Add(8, goal.NewRandomLookAround())
	if z.BehaviourGoals != nil {
		z.BehaviourGoals(z, m)
		return
	}
	z.AddBehaviourGoals(m)
}

// AddBehaviourGoals is Zombie.addBehaviourGoals. (The spear goal and MoveThroughVillageGoal are still to come.)
func (z *Zombie) AddBehaviourGoals(m *javamob.Mob) {
	m.Goals().Add(3, NewZombieAttack(1, false))
	m.Goals().Add(7, goal.NewWaterAvoidingRandomStroll(1, 0.001))
	m.Targets().Add(1, goal.NewHurtByTarget().SetAlertOthers("minecraft:zombified_piglin"))
	m.Targets().Add(2, goal.NewNearestAttackableTarget(nil, true, false))
	m.Targets().Add(3, goal.NewNearestAttackableTarget(goal.IsType("minecraft:villager", "minecraft:villager_v2", "minecraft:wandering_trader"), false, false))
	m.Targets().Add(3, goal.NewNearestAttackableTarget(goal.IsType("minecraft:iron_golem"), true, false))
	turtles := goal.NewNearestAttackableTarget(goal.IsType("minecraft:turtle"), true, false)
	turtles.Selector = babyOnLand
	m.Targets().Add(5, turtles)
}

// babyOnLand is Turtle.BABY_ON_LAND_SELECTOR.
func babyOnLand(e world.Entity, _ *world.Tx) bool {
	b, ok := e.(interface{ Baby() bool })
	if !ok || !b.Baby() {
		return false
	}
	if jm, ok := javamob.Of(e); ok {
		return !jm.InWater()
	}
	if bm, ok := mob.Of(e); ok {
		return !bm.Physics().InWater
	}
	return true
}

// NewZombieAttack is ZombieAttackGoal: a melee attack with the arms raised once ready to hit.
func NewZombieAttack(speed float64, follow bool) *goal.MeleeAttack {
	g := goal.NewMeleeAttack(speed, follow)
	raise := 0
	g.OnStart = func(*javamob.Mob, *goal.MeleeAttack) { raise = 0 }
	g.OnStop = func(m *javamob.Mob, _ *goal.MeleeAttack) { m.SetAggressive(false) }
	g.OnTick = func(m *javamob.Mob, g *goal.MeleeAttack) {
		raise++
		m.SetAggressive(raise >= 5 && g.TicksUntilNextAttack() < g.AttackInterval()/2)
	}
	return g
}

// CanBreakDoors is canBreakDoors.
func (z *Zombie) CanBreakDoors() bool { return z.canBreakDoors }

// SetCanBreakDoors is setCanBreakDoors: the door breaking goal and doors in paths.
func (z *Zombie) SetCanBreakDoors(m *javamob.Mob, v bool) {
	if z.canBreakDoors == v {
		return
	}
	z.canBreakDoors = v
	m.Navigation().SetCanOpenDoors(v)
	if v {
		m.Goals().Add(1, z.breakDoor)
	} else {
		m.Goals().Remove(m, z.breakDoor)
	}
}

// SetBaby is Zombie.setBaby: the baby speed bonus (+50%) and the baby dimensions.
func (z *Zombie) SetBaby(m *javamob.Mob, baby bool) {
	speed := m.Attributes().Instance(javamob.AttrMovementSpeed)
	speed.RemoveModifier("minecraft:baby")
	if baby {
		speed.AddModifier(javamob.Modifier{ID: "minecraft:baby", Amount: 0.5, Op: javamob.OpAddMultipliedBase, Transient: true})
		m.SetDimensions(float64(float32(0.49)), float64(float32(0.98)))
		m.SetEyeHeight(float64(float32(0.775)))
	} else {
		t := m.JavaType()
		m.SetDimensions(t.Width, t.Height)
		m.SetEyeHeight(t.EyeHeight)
	}
	m.SyncAttributes()
}

// XPReward is Zombie.getBaseExperienceReward: 2.5 times for a baby.
func (z *Zombie) XPReward(m *javamob.Mob, base int) int {
	if m.Baby() {
		return int(float64(base) * 2.5)
	}
	return base
}

// PostTick is Zombie.tick after super.tick: the drowning conversion.
func (z *Zombie) PostTick(m *javamob.Mob) {
	if z.ConvertsTo == "" || m.Dead() || m.NoAI() {
		return
	}
	if !m.UnderWater() && !eyeInWater(m) {
		z.inWaterTime = -1
		z.setConverting(m, false)
		return
	}
	if z.converting {
		if z.convertTime--; z.convertTime < 0 {
			z.convert(m)
		}
		return
	}
	if z.inWaterTime++; z.inWaterTime >= 600 {
		z.startConversion(m, 300)
	}
}

// eyeInWater is isEyeInFluid(WATER).
func eyeInWater(m *javamob.Mob) bool {
	p := m.Position()
	eye := cube.PosFromVec3(mgl64.Vec3{p[0], m.EyeY(), p[2]})
	l, ok := m.Tx().Liquid(eye)
	return ok && l.LiquidType() == "water"
}

func (z *Zombie) startConversion(m *javamob.Mob, t int) {
	z.convertTime = t
	z.setConverting(m, true)
}

func (z *Zombie) setConverting(m *javamob.Mob, v bool) {
	z.converting = v
	if m.Meta().Shaking != v {
		m.UpdateMeta(func(meta *mob.Meta) { meta.Shaking = v })
	}
}

// Converting is isUnderWaterConverting.
func (z *Zombie) Converting() bool { return z.converting }

// convert is ConversionTracker.doConversion: the zombie becomes ConvertsTo, keeping its equipment, and the new
// zombie rolls its attributes again (handleAttributes).
func (z *Zombie) convert(m *javamob.Mob) {
	n, ok := m.ConvertTo(z.ConvertsTo, true, func(n *javamob.Mob) {
		if nz, ok := n.Behaviour().(interface {
			HandleAttributes(m *javamob.Mob, special float64, reason javamob.SpawnReason)
		}); ok {
			nz.HandleAttributes(n, n.Difficulty().Special, javamob.SpawnConversion)
		}
	})
	if ok {
		// LevelEvent 1040: the zombie converted to a drowned.
		if bm, ok := mob.Of(n); ok {
			bm.PlaySound("convert_to_drowned")
		}
	}
}

// OnHurt is the end of Zombie.hurtServer: reinforcements on hard.
func (z *Zombie) OnHurt(m *javamob.Mob, _ float64, src world.DamageSource) {
	z.callReinforcements(m, src)
}

// callReinforcements is the reinforcement part of Zombie.hurtServer: on hard, with the spawn reinforcements
// chance, another zombie spawns 7-40 blocks away and joins the fight.
func (z *Zombie) callReinforcements(m *javamob.Mob, src world.DamageSource) {
	if m.Dead() || m.DifficultyID() != 3 {
		return
	}
	target, ok := m.Target()
	if !ok {
		if a := javamob.SourceEntity(src); a != nil {
			if _, living := a.(entity.Living); living {
				target, ok = a, true
			}
		}
	}
	if !ok || !(m.Rand().Float32() < float32(m.Attr(javamob.AttrSpawnReinforcements))) {
		return
	}
	t := m.JavaType()
	tx := m.Tx()
	r := m.Rand()
	p := cube.PosFromVec3(m.Position())
	nextInt := func(lo, hi int) int { return lo + r.IntN(hi-lo+1) }
	for i := 0; i < 50; i++ {
		pos := cube.Pos{
			p[0] + nextInt(7, 40)*nextInt(-1, 1),
			p[1] + nextInt(7, 40)*nextInt(-1, 1),
			p[2] + nextInt(7, 40)*nextInt(-1, 1),
		}
		if pos.OutOfBounds(tx.Range()) || !path.IsSolid(tx, pos.Side(cube.FaceDown)) || !t.CheckSpawnRules(tx, javamob.SpawnReinforcement, pos, r) {
			continue
		}
		at := mgl64.Vec3{float64(pos[0]), float64(pos[1]), float64(pos[2])}
		if playerNear(tx, at, 7) {
			continue
		}
		box := cube.Box(at[0]-t.Width/2, at[1], at[2]-t.Width/2, at[0]+t.Width/2, at[1]+t.Height, at[2]+t.Width/2)
		if path.Collides(tx, box) || liquidIn(tx, box) {
			continue
		}
		n, err := javamob.Spawn(tx, t.ID, at, javamob.SpawnReinforcement, javamob.SpawnOptions{Configure: func(n *javamob.Mob) {
			n.SetTarget(target)
		}})
		if err != nil {
			return
		}
		inst := m.Attributes().Instance(javamob.AttrSpawnReinforcements)
		old, _ := inst.Modifier("minecraft:reinforcement_caller_charge")
		inst.AddModifier(javamob.Modifier{ID: "minecraft:reinforcement_caller_charge", Amount: old.Amount - 0.05, Op: javamob.OpAddValue})
		n.Attributes().Instance(javamob.AttrSpawnReinforcements).AddModifier(javamob.Modifier{ID: "minecraft:reinforcement_callee_charge", Amount: float64(float32(-0.05)), Op: javamob.OpAddValue})
		return
	}
}

func playerNear(tx *world.Tx, at mgl64.Vec3, d float64) bool {
	for p := range tx.Players() {
		if l, ok := p.(entity.Living); ok && l.Dead() {
			continue
		}
		if p.Position().Sub(at).LenSqr() < d*d {
			return true
		}
	}
	return false
}

func liquidIn(tx *world.Tx, box cube.BBox) bool {
	for pos := range cube.Range3D(cube.PosFromVec3(box.Min()), cube.PosFromVec3(box.Max())) {
		if _, ok := tx.Liquid(pos); ok {
			return true
		}
	}
	return false
}

// DoHurtTarget is Zombie.doHurtTarget: a burning zombie without a weapon may set the target on fire.
func (z *Zombie) DoHurtTarget(m *javamob.Mob, target world.Entity) bool {
	if !m.BaseDoHurtTarget(target) {
		return false
	}
	eff := m.Difficulty().Effective
	if main, _ := m.HeldItems(); main.Empty() && m.IsOnFire() && m.Rand().Float32() < float32(eff)*0.3 {
		if jt, ok := javamob.Of(target); ok {
			jt.IgniteForSeconds(float64(2 * int(eff)))
		} else if f, ok := target.(entity.Flammable); ok {
			f.SetOnFire(secondsDuration(2 * int(eff)))
		}
	}
	return true
}

// FinalizeSpawn is Zombie.finalizeSpawn: picking up loot, babies (5%), door breaking, equipment, attributes.
func (z *Zombie) FinalizeSpawn(m *javamob.Mob, reason javamob.SpawnReason) {
	r := m.Rand()
	d := m.Difficulty()
	special := float32(d.Special)
	if reason != javamob.SpawnConversion {
		m.SetCanPickUpLoot(r.Float32() < 0.55*special)
	}
	if r.Float32() < 0.05 {
		m.SetBaby(true)
	}
	z.SetCanBreakDoors(m, r.Float32() < special*0.1)
	if reason != javamob.SpawnConversion {
		z.populateEquipment(m, d)
	}
	z.HandleAttributes(m, d.Special, reason)
}

// populateEquipment is Zombie.populateDefaultEquipmentSlots: random armour, and 1% (hard 5%) a weapon.
func (z *Zombie) populateEquipment(m *javamob.Mob, d javamob.DifficultyInstance) {
	m.PopulateDefaultEquipmentSlots(d)
	if z.ExtraEquipment != nil {
		z.ExtraEquipment(z, m, d)
		return
	}
	r := m.Rand()
	chance := float32(0.01)
	if m.DifficultyID() == 3 {
		chance = 0.05
	}
	if r.Float32() < chance {
		var it world.Item = item.Shovel{Tier: item.ToolTierIron}
		switch r.IntN(6) {
		case 0:
			it = item.Sword{Tier: item.ToolTierIron}
		case 1:
			if spear, ok := world.ItemByName("minecraft:iron_spear", 0); ok {
				it = spear
			}
		}
		m.SetItemSlot(javamob.SlotMainHand, item.NewStack(it, 1))
	}
}

// HandleAttributes is Zombie.handleAttributes: the random reinforcements chance, knockback resistance and follow
// range bonuses, and now and then a leader zombie (more health, more reinforcements, breaks doors).
func (z *Zombie) HandleAttributes(m *javamob.Mob, special float64, reason javamob.SpawnReason) {
	r := m.Rand()
	attrs := m.Attributes()
	attrs.Instance(javamob.AttrSpawnReinforcements).SetBase(r.Float64() * float64(float32(0.1)))
	attrs.Instance(javamob.AttrKnockbackResistance).AddModifier(javamob.Modifier{ID: "minecraft:random_spawn_bonus", Amount: r.Float64() * float64(float32(0.05)), Op: javamob.OpAddValue})
	if f := r.Float64() * 1.5 * special; f > 1 {
		attrs.Instance(javamob.AttrFollowRange).AddModifier(javamob.Modifier{ID: "minecraft:zombie_random_spawn_bonus", Amount: f, Op: javamob.OpAddMultipliedTotal})
	}
	if r.Float32() < float32(special)*0.05 {
		attrs.Instance(javamob.AttrSpawnReinforcements).AddModifier(javamob.Modifier{ID: "minecraft:leader_zombie_bonus", Amount: r.Float64()*0.25 + 0.5, Op: javamob.OpAddValue})
		attrs.Instance(javamob.AttrMaxHealth).AddModifier(javamob.Modifier{ID: "minecraft:leader_zombie_bonus", Amount: r.Float64()*3 + 1, Op: javamob.OpAddMultipliedTotal})
		m.SyncAttributes()
		if reason != javamob.SpawnConversion && reason != javamob.SpawnLoad && reason != javamob.SpawnDimensionTravel {
			m.SetHealth(m.MaxHealth())
		}
		z.SetCanBreakDoors(m, true)
	}
	m.SyncAttributes()
}

// WalkTargetValue is Monster.getWalkTargetValue.
func (z *Zombie) WalkTargetValue(m *javamob.Mob, pos cube.Pos) float64 {
	return javamob.MonsterWalkTargetValue(m, pos)
}

// EncodeNBT is Zombie.addAdditionalSaveData.
func (z *Zombie) EncodeNBT(data map[string]any) {
	data["CanBreakDoors"] = z.canBreakDoors
	conv := int32(-1)
	if z.converting {
		conv = int32(z.convertTime)
	}
	data["DrownedConversionTime"] = conv
	data["InWaterTime"] = int32(z.inWaterTime)
}

// DecodeNBT is Zombie.readAdditionalSaveData.
func (z *Zombie) DecodeNBT(m *javamob.Mob, data map[string]any) {
	if v, ok := data["CanBreakDoors"].(bool); ok {
		z.SetCanBreakDoors(m, v)
	}
	if v, ok := data["InWaterTime"].(int32); ok {
		z.inWaterTime = int(v)
	}
	if v, ok := data["DrownedConversionTime"].(int32); ok && v != -1 {
		z.startConversion(m, int(v))
	}
	if m.Baby() {
		z.SetBaby(m, true)
	}
}

// secondsDuration is n seconds.
func secondsDuration(n int) time.Duration { return time.Duration(n) * time.Second }
