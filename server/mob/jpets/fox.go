//go:build crossfly_full

package jpets

import (
	"math"
	"slices"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/janimal"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// Foxes (animal/fox/Fox.java): red and snow foxes that sleep by day in the
// shade, pounce on chickens and rabbits, eat berries, carry an item in their
// mouth, trust the players that bred them and defend them.

func init() {
	javamob.Register(javamob.Type{
		ID: "minecraft:fox", Category: javamob.CategoryCreature, Width: 0.6, Height: 0.7, EyeHeight: 0.4,
		Attributes: func() map[javamob.Attribute]float64 {
			a := animalAttributes(10, f32(0.3))()
			a[javamob.AttrAttackDamage] = 2
			a[javamob.AttrSafeFallDistance] = 5
			a[javamob.AttrFollowRange] = 32
			return a
		},
		Families: []string{"fox", "mob"},
		New:      func() javamob.Behaviour { return &Fox{} },
	})
}

// Fox flags (DATA_FLAGS_ID), shown with the Bedrock flags javasession reads.
const (
	foxSitting     = 1
	foxCrouching   = 4
	foxInterested  = 8
	foxPouncing    = 16
	foxSleeping    = 32
	foxFaceplanted = 64
	foxDefending   = 128
)

// Fox is the behaviour of a fox.
type Fox struct {
	Animal janimal.Animal
	// Snow is Fox.Variant SNOW (else RED).
	Snow    bool
	flags   byte
	trusted []uuid.UUID

	ticksSinceEaten int
	crouchAmount    float32
	targetsSet      bool
	panic           *janimal.PanicGoal[petHost]
	self            *world.EntityHandle
}

func (f *Fox) animal() *janimal.Animal { return &f.Animal }
func (f *Fox) panicking() bool         { return f.panic != nil && f.panic.Running() }

func (f *Fox) flag(b byte) bool { return f.flags&b != 0 }

// setFlag sets a DATA_FLAGS_ID bit and its client flag.
func (f *Fox) setFlag(m *javamob.Mob, b byte, v bool) {
	if v {
		f.flags |= b
	} else {
		f.flags &^= b
	}
	switch b {
	case foxSitting:
		if m.Meta().Sitting != v {
			m.UpdateMeta(func(meta *mob.Meta) { meta.Sitting = v })
		}
	case foxCrouching:
		m.SetActorFlag(protocol.EntityDataFlagStalking, v)
	case foxInterested:
		m.SetActorFlag(protocol.EntityDataFlagInterested, v)
	case foxSleeping:
		m.SetActorFlag(protocol.EntityDataFlagSleeping, v)
	}
}

// clearStates is Fox.clearStates.
func (f *Fox) clearStates(m *javamob.Mob) {
	for _, b := range []byte{foxInterested, foxCrouching, foxSitting, foxSleeping, foxDefending, foxFaceplanted} {
		f.setFlag(m, b, false)
	}
}

// canMove is Fox.canMove.
func (f *Fox) canMove() bool {
	return !f.flag(foxSleeping) && !f.flag(foxSitting) && !f.flag(foxFaceplanted)
}

// trusts is Fox.trusts.
func (f *Fox) trusts(e world.Entity) bool {
	return e != nil && slices.Contains(f.trusted, e.H().UUID())
}

// addTrusted is Fox.addTrustedEntity: two slots, the second one replaced.
func (f *Fox) addTrusted(id uuid.UUID) {
	if len(f.trusted) >= 1 {
		f.trusted = append(f.trusted[:1], id)
		return
	}
	f.trusted = append(f.trusted, id)
}

func (f *Fox) setVariant(m *javamob.Mob, snow bool) {
	f.Snow = snow
	v := int32(0)
	if snow {
		v = 1
	}
	m.UpdateMeta(func(meta *mob.Meta) { meta.Variant = v })
}

// RegisterGoals is Fox.registerGoals (the prey target goals come with the
// variant: setTargetGoals).
func (f *Fox) RegisterGoals(m *javamob.Mob) {
	f.self = m.H()
	m.SetPathfindingMalus(path.DamagingInNeighbour, 0)
	m.SetPathfindingMalus(path.Damaging, 0)
	m.SetCanPickUpLoot(true)
	m.Navigation().SetRequiredPathLength(m, 32)
	gs, ts := m.Goals(), m.Targets()
	float := goal.NewFloat(m)
	gs.Add(0, &foxFloat{Float: float, f: f})
	gs.Add(1, &foxFaceplant{f: f})
	f.panic = &janimal.PanicGoal[petHost]{Speed: 2.2, ShouldPanic: func(h petHost) bool {
		_, ok := janimal.PanicCauses[h.LastDamageType()]
		return !f.flag(foxDefending) && ok
	}}
	gs.Add(2, wrapGoal(f.panic))
	gs.Add(3, &foxBreed{animalGoal: animalGoal{g: &janimal.BreedGoal[petHost]{Speed: 1}}, f: f})
	gs.Add(4, goal.NewAvoidEntity(func(e world.Entity) bool {
		p, ok := e.(*player.Player)
		return ok && !p.Sneaking() && !p.GameMode().CreativeInventory() && p.GameMode() != world.GameModeSpectator &&
			!f.trusts(e) && !f.flag(foxDefending)
	}, 16, 1.6, 1.4))
	gs.Add(4, goal.NewAvoidEntity(func(e world.Entity) bool {
		if entityID(e) != "minecraft:wolf" || f.flag(foxDefending) {
			return false
		}
		jm, ok := javamob.Of(e)
		w, isWolf := jm.Behaviour().(*Wolf)
		return ok && isWolf && !w.Tame
	}, 8, 1.6, 1.4))
	gs.Add(4, goal.NewAvoidEntity(func(e world.Entity) bool {
		return entityID(e) == "minecraft:polar_bear" && !f.flag(foxDefending)
	}, 8, 1.6, 1.4))
	gs.Add(5, &foxStalk{f: f})
	gs.Add(6, &foxPounce{f: f})
	gs.Add(6, &foxSeekShelter{f: f, speed: 1.25, interval: javamob.ReducedTickDelay(100)})
	melee := goal.NewMeleeAttack(float64(float32(1.2)), true)
	melee.Attack = func(m *javamob.Mob, g *goal.MeleeAttack, t world.Entity) {
		if g.TicksUntilNextAttack() <= 0 && m.IsWithinMeleeAttackRange(t) && m.Sensing().HasLineOfSight(m, t) {
			g.ResetAttackCooldown()
			m.DoHurtTarget(t)
			m.PlaySound("bite")
		}
	}
	melee.OnStart = func(m *javamob.Mob, _ *goal.MeleeAttack) { f.setFlag(m, foxInterested, false) }
	gs.Add(7, &foxMelee{MeleeAttack: melee, f: f})
	gs.Add(7, &foxSleep{f: f, countdown: m.Rand().IntN(javamob.ReducedTickDelay(140))})
	gs.Add(8, &foxFollowParent{animalGoal: animalGoal{g: &janimal.FollowParentGoal[petHost]{Speed: 1.25}}, f: f})
	gs.Add(10, &moveToBlock{speed: float64(float32(1.2)), searchRange: 12, verticalRange: 1, flags: javamob.FlagMove | javamob.FlagJump,
		valid: foxBerries, hooks: foxEatBerriesHooks(f), acceptedDistance: 2, recalc: 100})
	gs.Add(10, goal.NewLeapAtTarget(0.4))
	gs.Add(11, goal.NewWaterAvoidingRandomStroll(1, 0.001))
	gs.Add(11, &foxSearchItems{f: f})
	gs.Add(12, &foxLookAtPlayer{LookAt: goal.NewLookAtPlayer(24), f: f})
	gs.Add(13, &foxPerch{f: f})
	ts.Add(3, &foxDefendTrusted{f: f, Target: goal.Target{GoalBase: javamob.GoalBase{Control: javamob.FlagTarget}, UnseenMemoryTicks: 60}})
}

// setTargetGoals is Fox.setTargetGoals: a red fox hunts chickens, rabbits
// and baby turtles first, a snow fox fish.
func (f *Fox) setTargetGoals(m *javamob.Mob) {
	if f.targetsSet {
		return
	}
	f.targetsSet = true
	land := goal.NewNearestAttackableTarget(func(e world.Entity) bool {
		id := entityID(e)
		return id == "minecraft:chicken" || id == "minecraft:rabbit"
	}, false, false)
	turtle := goal.NewNearestAttackableTarget(isBabyTurtleOnLand, false, false)
	fish := goal.NewNearestAttackableTarget(func(e world.Entity) bool {
		switch entityID(e) {
		case "minecraft:cod", "minecraft:salmon", "minecraft:tropical_fish":
			return true // AbstractSchoolingFish
		}
		return false
	}, false, false)
	fish.RandomInterval = javamob.ReducedTickDelay(20)
	ts := m.Targets()
	if !f.Snow {
		ts.Add(4, land)
		ts.Add(4, turtle)
		ts.Add(6, fish)
	} else {
		ts.Add(4, fish)
		ts.Add(6, land)
		ts.Add(6, turtle)
	}
}

// FinalizeSpawn is Fox.finalizeSpawn: snow foxes in #spawns_snow_foxes, an
// item in the mouth one time in five.
func (f *Fox) FinalizeSpawn(m *javamob.Mob, reason javamob.SpawnReason) {
	f.setVariant(m, slices.Contains(SnowFoxBiomes, spawnContextAt(m).Biome()))
	f.setTargetGoals(m)
	r := m.Rand()
	if r.Float32() < 0.2 {
		odds := r.Float32()
		name := "minecraft:feather"
		switch {
		case odds < 0.05:
			name = "minecraft:emerald"
		case odds < 0.2:
			name = "minecraft:egg"
		case odds < 0.4:
			if r.IntN(2) == 0 {
				name = "minecraft:rabbit_foot"
			} else {
				name = "minecraft:rabbit_hide"
			}
		case odds < 0.6:
			name = "minecraft:wheat"
		case odds < 0.8:
			name = "minecraft:leather"
		}
		if it, ok := world.ItemByName(name, 0); ok {
			m.SetItemSlot(javamob.SlotMainHand, item.NewStack(it, 1))
		}
	}
}

// AiStep is Fox.aiStep: eating the food it carries after 30 seconds, crouch
// and interest off without a target, and picking up items (Mob.aiStep with
// canPickUpLoot).
func (f *Fox) AiStep(m *javamob.Mob) {
	animalTick(m, &f.Animal)
	if m.Dead() {
		return
	}
	f.ticksSinceEaten++
	mouth := m.ItemBySlot(javamob.SlotMainHand)
	_, hasTarget := m.Target()
	if f.canEat(m, mouth, hasTarget) {
		if f.ticksSinceEaten > 600 {
			m.Heal(foodNutrition(itemName(mouth), 2), entity.FoodHealingSource{})
			m.SetItemSlot(javamob.SlotMainHand, mouth.Grow(-1))
			f.ticksSinceEaten = 0
		} else if f.ticksSinceEaten > 560 && m.Rand().Float32() < 0.1 {
			m.PlaySound("eat")
		}
	}
	if !hasTarget {
		f.setFlag(m, foxCrouching, false)
		f.setFlag(m, foxInterested, false)
	}
	if f.flag(foxSleeping) {
		m.SetJumping(false)
		m.SetXxa(0)
		m.SetZza(0)
	}
	if f.flag(foxDefending) && m.Rand().Float32() < 0.05 {
		m.PlaySound("mad")
	}
	f.pickUpItems(m)
}

// canEat is Fox.canEat: food with no target, on the ground, awake.
func (f *Fox) canEat(m *javamob.Mob, s item.Stack, hasTarget bool) bool {
	_, isFood := nutrition[itemName(s)]
	return !s.Empty() && isFood && !hasTarget && m.OnGround() && !f.flag(foxSleeping)
}

// pickUpItems is Mob.aiStep's loot pickup for a fox (canPickUpLoot): items
// touching its box grown by 1, 0, 1 that it may hold (Fox.canHoldItem).
func (f *Fox) pickUpItems(m *javamob.Mob) {
	box := m.BBox().GrowVec3(mgl64.Vec3{1, 0, 1})
	for _, e := range entitiesTouching(m.Tx(), box) {
		ent, ok := e.(*entity.Ent)
		if !ok {
			continue
		}
		ib, ok := ent.Behaviour().(*entity.ItemBehaviour)
		if !ok || !ib.PickupReady() {
			continue
		}
		f.pickUpItem(m, ent, ib.Item())
		return
	}
}

// pickUpItem is Fox.pickUpItem: hold one, drop the rest, spit out what was
// in the mouth.
func (f *Fox) pickUpItem(m *javamob.Mob, e *entity.Ent, s item.Stack) {
	held := m.ItemBySlot(javamob.SlotMainHand)
	_, newFood := nutrition[itemName(s)]
	_, heldFood := nutrition[itemName(held)]
	if !held.Empty() && !(f.ticksSinceEaten > 0 && newFood && !heldFood) {
		return
	}
	tx := m.Tx()
	if s.Count() > 1 {
		tx.AddEntity(tx.World().EntityRegistry().Config().Item(world.EntitySpawnOpts{Position: m.Position()}, s.Grow(-1)))
	}
	if !held.Empty() {
		yaw := m.YRot() * math.Pi / 180
		at := m.Position().Add(mgl64.Vec3{-math.Sin(yaw), 1, math.Cos(yaw)})
		tx.AddEntity(tx.World().EntityRegistry().Config().Item(world.EntitySpawnOpts{Position: at}, held))
		m.PlaySound("spit")
	}
	m.SetItemSlot(javamob.SlotMainHand, s.Grow(1-s.Count()))
	m.SetGuaranteedDrop(javamob.SlotMainHand)
	_ = e.Close()
	f.ticksSinceEaten = 0
}

// PostTick is Fox.tick: waking up in water, with a target or in a
// thunderstorm; no sitting while asleep or swimming; the crouch build-up.
func (f *Fox) PostTick(m *javamob.Mob) {
	_, hasTarget := m.Target()
	inWater := m.InWater()
	if inWater || hasTarget || m.Tx().Thundering() {
		f.setFlag(m, foxSleeping, false)
	}
	if inWater || f.flag(foxSleeping) {
		f.setFlag(m, foxSitting, false)
	}
	if f.flag(foxCrouching) {
		f.crouchAmount = min(f.crouchAmount+0.2, 5)
	} else {
		f.crouchAmount = 0
	}
}

// CustomServerAiStep: FoxMoveControl does not move a fox that cannot move.
func (f *Fox) CustomServerAiStep(m *javamob.Mob) {
	if !f.canMove() {
		m.MoveControl().SetWait()
	}
	m.UpdateMeta(func(meta *mob.Meta) { meta.Sneaking = f.flag(foxCrouching) })
}

// MobInteract: Animal.mobInteract with sweet and glow berries.
func (f *Fox) MobInteract(m *javamob.Mob, user item.User, held item.Stack) (item.Stack, bool) {
	p, ok := user.(*player.Player)
	if !ok {
		return held, false
	}
	n := itemName(held)
	return animalFeed(m, &f.Animal, p, held, n == "minecraft:sweet_berries" || n == "minecraft:glow_berries")
}

// OnHurt: love ends.
func (f *Fox) OnHurt(m *javamob.Mob, dealt float64, src world.DamageSource) { f.Animal.ResetLove() }

// DropCustomDeathLoot is Fox.dropAllDeathLoot: the item in the mouth.
func (f *Fox) DropCustomDeathLoot(m *javamob.Mob, _ world.DamageSource, _ bool) {
	if s := m.ItemBySlot(javamob.SlotMainHand); !s.Empty() {
		m.SpawnAtLocation(s)
		m.SetItemSlot(javamob.SlotMainHand, item.Stack{})
	}
}

// breed is Fox.getBreedOffspring with FoxBreedGoal.breed: either parent's
// variant; the cub trusts the players that fed its parents.
func (f *Fox) breed(m, partner *javamob.Mob) bool {
	baby, err := javamob.Spawn(m.Tx(), "minecraft:fox", m.Position(), javamob.SpawnBreeding, javamob.SpawnOptions{})
	if err != nil {
		return false
	}
	pf := partner.Behaviour().(*Fox)
	bf := baby.Behaviour().(*Fox)
	snow := pf.Snow
	if m.Rand().IntN(2) == 0 {
		snow = f.Snow
	}
	bf.setVariant(baby, snow)
	a, b := f.Animal.LoveCause(), pf.Animal.LoveCause()
	if a != uuid.Nil {
		bf.addTrusted(a)
	}
	if b != uuid.Nil && b != a {
		bf.addTrusted(b)
	}
	bf.Animal.SetBaby(true)
	baby.SetBaby(true)
	return true
}

// EncodeNBT and DecodeNBT: Trusted, Sleeping, Type, Sitting, Crouching.
func (f *Fox) EncodeNBT(data map[string]any) {
	f.Animal.EncodeNBT(data)
	var trusted []any
	for _, id := range f.trusted {
		trusted = append(trusted, janimal.UUIDInts(id))
	}
	data["Trusted"] = trusted
	data["Sleeping"] = boolByte(f.flag(foxSleeping))
	data["Type"] = map[bool]string{false: "red", true: "snow"}[f.Snow]
	data["Sitting"] = boolByte(f.flag(foxSitting))
	data["Crouching"] = boolByte(f.flag(foxCrouching))
}

func (f *Fox) DecodeNBT(m *javamob.Mob, data map[string]any) {
	f.Animal.DecodeNBT(data)
	m.SetBaby(f.Animal.Baby())
	f.trusted = nil
	if list, ok := data["Trusted"].([]any); ok {
		for _, v := range list {
			if id := janimal.NBTUUID(map[string]any{"id": v}, "id"); id != uuid.Nil {
				f.addTrusted(id)
			}
		}
	}
	f.setFlag(m, foxSleeping, janimal.NBTBool(data, "Sleeping", false))
	t, _ := data["Type"].(string)
	f.setVariant(m, t == "snow")
	f.setFlag(m, foxSitting, janimal.NBTBool(data, "Sitting", false))
	f.setFlag(m, foxCrouching, janimal.NBTBool(data, "Crouching", false))
	f.setTargetGoals(m)
}

// isPathClear is Fox.isPathClear: six steps towards the target, three blocks
// high, all replaceable.
func isPathClear(m *javamob.Mob, t world.Entity) bool {
	p, tp := m.Position(), t.Position()
	zd, xd := tp[2]-p[2], tp[0]-p[0]
	slope := zd / xd
	for i := 0; i < 6; i++ {
		var x, z float64
		if slope == 0 || math.IsNaN(slope) {
			x = xd * float64(float32(i)/6)
		} else {
			z = zd * float64(float32(i)/6)
			x = z / slope
		}
		for j := 1; j < 4; j++ {
			pos := cube.PosFromVec3(mgl64.Vec3{p[0] + x, p[1] + float64(j), p[2] + z})
			if r, ok := m.Tx().Block(pos).(block.Replaceable); !ok || !r.ReplaceableBy(block.Air{}) {
				if _, air := m.Tx().Block(pos).(block.Air); !air {
					return false
				}
			}
		}
	}
	return true
}
