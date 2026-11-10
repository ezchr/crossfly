//go:build crossfly_full

package jpets

import (
	"math"
	"slices"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/janimal"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// The horse family (AbstractHorse and its classes Horse, Donkey, Mule,
// SkeletonHorse, ZombieHorse, AbstractChestedHorse; Llama in llama.go).

// equineKind is the Java class of an equine.
type equineKind uint8

const (
	kindHorse equineKind = iota
	kindDonkey
	kindMule
	kindSkeletonHorse
	kindZombieHorse
	kindLlama
)

// equineInfo is what differs between the classes.
type equineInfo struct {
	id string
	// chested: AbstractChestedHorse (donkey, mule, llama).
	chested bool
	// seat is the Bedrock seat (minecraft:rideable) of the adult.
	seat mgl64.Vec3
	// maxTemper is getMaxTemper.
	maxTemper int
	// canRear is canPerformRearing (llamas do not).
	canRear bool
	// eatsGrass is canEatGrass (llamas do not).
	eatsGrass bool
	// armour is the body armour the type wears (can_wear_horse_armor;
	// llamas wear carpets).
	armour func(name string) bool
	// food is isFood.
	food func(name string) bool
}

func horseArmour(name string) bool {
	switch name {
	case "minecraft:leather_horse_armor", "minecraft:copper_horse_armor", "minecraft:iron_horse_armor",
		"minecraft:golden_horse_armor", "minecraft:diamond_horse_armor", "minecraft:netherite_horse_armor":
		return true
	}
	return false
}

func noArmour(string) bool { return false }

var equines = map[equineKind]equineInfo{
	kindHorse:         {id: "minecraft:horse", seat: mgl64.Vec3{0, 1.1, -0.2}, maxTemper: MaxTemperHorse, canRear: true, eatsGrass: true, armour: horseArmour, food: IsHorseFood},
	kindDonkey:        {id: "minecraft:donkey", chested: true, seat: mgl64.Vec3{0, 0.925, -0.2}, maxTemper: MaxTemperHorse, canRear: true, eatsGrass: true, armour: noArmour, food: IsHorseFood},
	kindMule:          {id: "minecraft:mule", chested: true, seat: mgl64.Vec3{0, 0.975, -0.2}, maxTemper: MaxTemperHorse, canRear: true, eatsGrass: true, armour: noArmour, food: IsHorseFood},
	kindSkeletonHorse: {id: "minecraft:skeleton_horse", seat: mgl64.Vec3{0, 1.1, -0.2}, maxTemper: MaxTemperHorse, canRear: true, eatsGrass: true, armour: noArmour, food: IsHorseFood},
	kindZombieHorse:   {id: "minecraft:zombie_horse", seat: mgl64.Vec3{0, 1.1, -0.2}, maxTemper: MaxTemperHorse, canRear: true, eatsGrass: true, armour: horseArmour, food: func(n string) bool { return n == "minecraft:red_mushroom" }},
	kindLlama:         {id: "minecraft:llama", chested: true, seat: mgl64.Vec3{0, 1.17, -0.3}, maxTemper: MaxTemperLlama, eatsGrass: false, armour: IsCarpet, food: IsLlamaFood},
}

// IsCarpet reports whether name is a wool carpet (a llama's body armour:
// the carpets' equippable component allows llamas).
func IsCarpet(name string) bool {
	return len(name) > len("minecraft:_carpet") && name[len(name)-7:] == "_carpet" && name != "minecraft:moss_carpet" && name != "minecraft:pale_moss_carpet"
}

// Equine is the behaviour of a horse, donkey, mule, skeleton horse, zombie
// horse or llama (AbstractHorse's fields).
type Equine struct {
	kind equineKind
	// Animal is the AgeableMob/Animal state (janimal).
	Animal janimal.Animal

	Tame, Bred         bool
	Temper             int
	eating, standing   bool
	mouthOpen          bool
	eatingCounter      int
	mouthCounter       int
	standCounter       int
	tailCounter        int
	sprintCounter      int
	gallopSoundCounter int

	// Saddle is the SADDLE slot.
	Saddle item.Stack
	// Chest is AbstractChestedHorse.hasChest; inv the chest slots.
	Chest bool
	inv   *inventory.Inventory

	// Variant and Markings are the horse coat (Horse.DATA_ID_TYPE_VARIANT),
	// Variant also the llama colour.
	Variant, Markings int

	jumpPending       float64
	allowStandSliding bool

	llama    llamaState
	skeleton skeletonState
	panic    *janimal.PanicGoal[petHost]
}

func (h *Equine) info() equineInfo { return equines[h.kind] }

// IsTamedHorse reports whether the equine is tamed (AbstractHorse.isTamed;
// Wolf.wantsToAttack leaves tamed horses alone).
func (h *Equine) IsTamedHorse() bool { return h.Tame }

// maxTemper is getMaxTemper (Paper keeps it as maxDomestication, saved as
// Bukkit.MaxDomestication; the default is the vanilla value).
func (h *Equine) maxTemper() int { return h.info().maxTemper }

// setTamed is AbstractHorse.setTamed (flag 0x02), Meta.Tamed for the
// clients.
func (h *Equine) setTamed(m *javamob.Mob, v bool) {
	h.Tame = v
	if m.Meta().Tamed != v {
		m.UpdateMeta(func(meta *mob.Meta) { meta.Tamed = v })
	}
}

// setEating is setEating (flag 0x10): the head down eating grass, the Bedrock
// eating flag.
func (h *Equine) setEating(m *javamob.Mob, v bool) {
	h.eating = v
	if m.ActorFlag(protocol.EntityDataFlagEating) != v {
		m.SetActorFlag(protocol.EntityDataFlagEating, v)
	}
}

// setStanding is setStanding/clearStanding (flag 0x20): rearing.
func (h *Equine) setStanding(m *javamob.Mob, ticks int) {
	h.setEating(m, false)
	h.standing = true
	h.standCounter = ticks
	if !m.ActorFlag(protocol.EntityDataFlagStanding) {
		m.SetActorFlag(protocol.EntityDataFlagStanding, true)
	}
}

func (h *Equine) clearStanding(m *javamob.Mob) {
	h.standing = false
	h.standCounter = 0
	if m.ActorFlag(protocol.EntityDataFlagStanding) {
		m.SetActorFlag(protocol.EntityDataFlagStanding, false)
	}
}

// standIfPossible is AbstractHorse.standIfPossible: rear for 20 ticks.
func (h *Equine) standIfPossible(m *javamob.Mob) {
	if h.info().canRear {
		h.setStanding(m, 20)
	}
}

// makeMad is AbstractHorse.makeMad: rear and play the angry sound.
func (h *Equine) makeMad(m *javamob.Mob) {
	if !h.standing {
		h.standIfPossible(m)
		m.PlaySound("mad")
	}
}

// openMouth is AbstractHorse.openMouth (30 ticks).
func (h *Equine) openMouth() {
	h.mouthCounter = 1
	h.mouthOpen = true
}

// saddled is isSaddled.
func (h *Equine) saddled() bool { return itemName(h.Saddle) == "minecraft:saddle" }

// setSaddle equips or removes the saddle (Meta.Saddled for both clients).
func (h *Equine) setSaddle(m *javamob.Mob, s item.Stack) {
	h.Saddle = s
	v := h.saddled()
	if m.Meta().Saddled != v {
		m.UpdateMeta(func(meta *mob.Meta) { meta.Saddled = v })
	}
	// What the Bedrock horse gets in its saddled component group
	// (input_ground_controlled, can_power_jump): the Bedrock client then
	// steers it and shows the jump bar.
	steer := v && h.kind != kindLlama
	if m.ActorFlag(protocol.EntityDataFlagKeyboardControlled) != steer {
		m.SetActorFlag(protocol.EntityDataFlagKeyboardControlled, steer)
		m.SetActorFlag(protocol.EntityDataFlagPowerJump, steer)
	}
}

// setChest is AbstractChestedHorse.setChest: the chest flag and a new
// inventory size (createInventory keeps what fits).
func (h *Equine) setChest(m *javamob.Mob, v bool) {
	h.Chest = v
	if m.Meta().Chested != v {
		m.UpdateMeta(func(meta *mob.Meta) { meta.Chested = v })
	}
	h.createInventory()
}

// inventoryColumns is getInventoryColumns: 5 for a chested donkey or mule,
// the strength for a llama.
func (h *Equine) inventoryColumns() int {
	switch {
	case !h.info().chested || !h.Chest:
		return 0
	case h.kind == kindLlama:
		return h.llama.Strength
	}
	return 5
}

// createInventory is AbstractHorse.createInventory: 3 rows of the columns.
func (h *Equine) createInventory() {
	n := h.inventoryColumns() * 3
	old := h.inv
	h.inv = inventory.New(max(n, 1), nil)
	if old != nil {
		for i := 0; i < min(old.Size(), n); i++ {
			if it, err := old.Item(i); err == nil && !it.Empty() {
				_ = h.inv.SetItem(i, it)
			}
		}
	}
}

// ---- Behaviour hooks ----

// AiStep is AbstractHorse.aiStep: the tail flick, healing 1 every 900
// ticks on average, eating grass and following the mother.
func (h *Equine) AiStep(m *javamob.Mob) {
	if m.Rand().IntN(200) == 0 {
		h.tailCounter = 1
	}
	if m.Dead() {
		return
	}
	if m.Rand().IntN(900) == 0 {
		m.Heal(1, entity.FoodHealingSource{})
	}
	if h.info().eatsGrass {
		if !h.eating && len(m.Passengers()) == 0 && m.Rand().IntN(300) == 0 {
			below := cube.PosFromVec3(m.Position()).Side(cube.FaceDown)
			if _, ok := m.Tx().Block(below).(block.Grass); ok {
				h.setEating(m, true)
			}
		}
		if h.eating {
			h.eatingCounter++
			if h.eatingCounter > 50 {
				h.eatingCounter = 0
				h.setEating(m, false)
			}
		}
	}
	h.followMommy(m)
	if h.kind == kindSkeletonHorse {
		h.skeletonAiStep(m)
	}
}

// followMommy is AbstractHorse.followMommy: a bred foal paths to the nearest
// bred adult equine within 16 blocks when more than 2 away (llamas only
// outside a caravan).
func (h *Equine) followMommy(m *javamob.Mob) {
	if !h.Bred || !m.Baby() || h.eating || (h.kind == kindLlama && h.llama.inCaravan()) {
		return
	}
	box := m.BBox().Grow(16)
	mom, ok := m.NearestEntity(box, func(e world.Entity) bool {
		o, ok := javamob.Of(e)
		if !ok {
			return false
		}
		b, ok := o.Behaviour().(*Equine)
		return ok && b.Bred
	}, javamob.TargetingConditions{Range: 16, IgnoreLineOfSight: true})
	if ok && distSq(m.Position(), mom.Position()) > 4 {
		m.Navigation().CreatePathToEntity(m, mom, 0)
	}
}

// PostTick is the rest of AbstractHorse.tick: the mouth, rearing, tail and
// sprint counters.
func (h *Equine) PostTick(m *javamob.Mob) {
	if h.mouthCounter > 0 {
		h.mouthCounter++
		if h.mouthCounter > 30 {
			h.mouthCounter = 0
			h.mouthOpen = false
		}
	}
	if h.standCounter > 0 {
		h.standCounter--
		if h.standCounter <= 0 {
			h.clearStanding(m)
		}
	}
	if h.tailCounter > 0 {
		h.tailCounter++
		if h.tailCounter > 8 {
			h.tailCounter = 0
		}
	}
	if h.sprintCounter > 0 {
		h.sprintCounter++
		if h.sprintCounter > 300 {
			h.sprintCounter = 0
		}
	}
}

// OnHurt is AbstractHorse.hurtServer: one hit in three makes it rear.
func (h *Equine) OnHurt(m *javamob.Mob, dealt float64, src world.DamageSource) {
	h.Animal.ResetLove()
	if m.Rand().IntN(3) == 0 {
		h.standIfPossible(m)
	}
}

// RemoveWhenFarAway: horses never despawn (Animal is persistent unless
// MobCategory says otherwise), except zombie horses (ZombieHorse
// .removeWhenFarAway true) and trap skeleton horses (Mob default).
func (h *Equine) RemoveWhenFarAway(m *javamob.Mob, distSqr float64) bool {
	return h.kind == kindZombieHorse
}

// FinalizeSpawn is AbstractHorse.finalizeSpawn (randomizeAttributes) and
// each class's own: the horse coat, the llama strength and colour.
func (h *Equine) FinalizeSpawn(m *javamob.Mob, reason javamob.SpawnReason) {
	r := m.Rand()
	a := m.Attributes()
	switch h.kind {
	case kindHorse:
		a.Instance(javamob.AttrMaxHealth).SetBase(GenerateMaxHealth(r))
		a.Instance(javamob.AttrMovementSpeed).SetBase(GenerateSpeed(r))
		a.Instance(javamob.AttrJumpStrength).SetBase(GenerateJumpStrength(r))
		v, mk := HorseSpawnLook(r, 0, false)
		h.setLook(m, v, mk)
	case kindDonkey, kindMule:
		a.Instance(javamob.AttrMaxHealth).SetBase(GenerateMaxHealth(r))
	case kindSkeletonHorse:
		a.Instance(javamob.AttrJumpStrength).SetBase(GenerateJumpStrength(r))
	case kindZombieHorse:
		a.Instance(javamob.AttrJumpStrength).SetBase(zombieHorseJump(r.Float64))
		a.Instance(javamob.AttrMovementSpeed).SetBase(zombieHorseSpeed(r.Float64))
	case kindLlama:
		a.Instance(javamob.AttrMaxHealth).SetBase(GenerateMaxHealth(r))
		h.llama.Strength = LlamaRandomStrength(r)
		h.setLook(m, LlamaSpawnVariant(r, 0, false), 0)
	}
	m.SetMaxHealth(a.Value(javamob.AttrMaxHealth))
	m.SetHealth(m.MaxHealth())
	// AgeableMobGroupData(0.2F): a fifth of the horses of a spawned group are
	// foals (the first of a group never is); spawn eggs make adults.
	if reason == javamob.SpawnNatural || reason == javamob.SpawnChunkGeneration {
		if r.Float32() < 0.2 {
			m.SetBaby(true)
		}
	}
}

// zombieHorseJump and zombieHorseSpeed are ZombieHorse.generateZombieHorse*.
func zombieHorseJump(p func() float64) float64 {
	return 0.5 + p()*0.06666666666666667 + p()*0.06666666666666667 + p()*0.06666666666666667
}

func zombieHorseSpeed(p func() float64) float64 {
	return (9 + p() + p() + p()) / float64(float32(42.16))
}

// setLook sets the coat (or llama colour) and markings for both clients.
func (h *Equine) setLook(m *javamob.Mob, variant, markings int) {
	h.Variant, h.Markings = variant, markings
	m.UpdateMeta(func(meta *mob.Meta) {
		meta.Variant, meta.MarkVariant = int32(variant), int32(markings)
	})
}

// ---- Interaction ----

// MobInteract is AbstractHorse.mobInteract with the overrides of Horse,
// AbstractChestedHorse, SkeletonHorse, ZombieHorse and Llama.
func (h *Equine) MobInteract(m *javamob.Mob, user item.User, held item.Stack) (item.Stack, bool) {
	p, ok := user.(*player.Player)
	if !ok {
		return held, false
	}
	if h.kind == kindSkeletonHorse && !h.Tame {
		return held, false // SkeletonHorse: PASS while wild
	}
	if h.kind == kindZombieHorse {
		m.SetPersistenceRequired() // ZombieHorse.interact
	}
	name := itemName(held)
	vehicle := len(m.Passengers()) > 0
	openInv := !m.Baby() && h.Tame && p.Sneaking()
	// Horse/Chested/Zombie: the food, rearing and chest part.
	if h.kind != kindSkeletonHorse && !vehicle && !openInv && (!m.Baby() || name != "minecraft:golden_dandelion") && !held.Empty() {
		if h.info().food(name) {
			if left, used := h.fedFood(m, p, held); used {
				return left, true
			}
			// PASS: the food did nothing; fall through to riding.
			return held, false
		} else if !h.Tame {
			h.makeMad(m)
			return held, true
		} else if h.info().chested && !h.Chest && name == "minecraft:chest" && !m.Baby() {
			h.setChest(m, true)
			m.PlaySound("armor") // DONKEY_CHEST / LLAMA_CHEST
			return consumeOne(user, held), true
		}
	}
	// AbstractHorse.mobInteract.
	if vehicle || m.Baby() {
		return h.animalInteract(m, p, held)
	}
	if h.Tame && p.Sneaking() {
		p.OpenEntityContainer(m)
		return held, true
	}
	if !held.Empty() {
		// ItemStack.interactLivingEntity: the saddle (Equippable.equipOnInteract).
		if name == "minecraft:saddle" && h.kind != kindLlama && h.Tame && !h.saddled() {
			h.setSaddle(m, held.Grow(1-held.Count()))
			m.PlaySound("saddle")
			return consumeOne(user, held), true
		}
		if h.info().armour(name) && m.BodyArmour().Empty() && (h.kind != kindLlama || h.Tame) {
			m.SetBodyArmour(held.Grow(1 - held.Count()))
			m.PlaySound("armor")
			return consumeOne(user, held), true
		}
	}
	h.doPlayerRide(m, p)
	return held, true
}

// animalInteract is Animal.mobInteract for a ridden mount or a foal: food
// makes a baby grow (fedFood covers the equines' own foods).
func (h *Equine) animalInteract(m *javamob.Mob, p *player.Player, held item.Stack) (item.Stack, bool) {
	if h.info().food(itemName(held)) {
		return h.fedFood(m, p, held)
	}
	return held, false
}

// fedFood is AbstractHorse.fedFood with handleEating.
func (h *Equine) fedFood(m *javamob.Mob, p *player.Player, held item.Stack) (item.Stack, bool) {
	f, ok := HorseFoodOf(itemName(held), h.kind == kindLlama)
	if !ok {
		// zombie horse red mushroom: the AbstractHorse row.
		f, ok = HorseFoodOf(itemName(held), false)
		if !ok {
			return held, false
		}
	}
	res := HandleEating(f, EatState{
		Health: m.Health(), MaxHealth: m.MaxHealth(), Baby: m.Baby(), AgeLocked: h.Animal.AgeLocked(),
		Tamed: h.Tame, Age: int(h.Animal.Age()), InLove: h.Animal.InLove(),
		CanFallInLove: h.Animal.CanFallInLove() && h.kind != kindZombieHorse,
		Temper:        h.Temper, MaxTemper: h.maxTemper(),
	}, h.kind == kindLlama)
	if !res.Used {
		return held, false
	}
	if res.Love {
		h.Animal.SetInLove(p.UUID())
		broadcastEvent(m, janimal.Event(janimal.EventLoveHearts))
	}
	if res.Heal > 0 {
		m.Heal(res.Heal, entity.FoodHealingSource{})
	}
	if res.AgeUp > 0 && h.kind != kindSkeletonHorse && h.kind != kindZombieHorse {
		if h.Animal.AgeUp(int32(res.AgeUp), false) {
			m.SetBaby(h.Animal.Baby())
		}
	}
	h.Temper = res.NewTemper
	// eating(): open mouth and the eat sound.
	h.openMouth()
	m.PlaySound("eat")
	return consumeOne(p, held), true
}

// doPlayerRide is AbstractHorse.doPlayerRide: stop eating and rearing, the
// rider takes the horse's rotation and gets on.
func (h *Equine) doPlayerRide(m *javamob.Mob, p *player.Player) {
	h.setEating(m, false)
	h.clearStanding(m)
	rideable, ok := entity.RideableOf(m)
	if !ok {
		return
	}
	slot, ok := rideable.NextFreeSeatIndex(m.Position())
	if !ok {
		return
	}
	p.MountEntity(m.Tx(), rideable, slot)
}

// ---- Riding (Mount) ----

// Seats implements Mount: one rider (camels and happy ghasts have their own).
func (h *Equine) Seats(m *javamob.Mob, n int) []mgl64.Vec3 {
	s := h.info().seat
	if m.Baby() {
		s = s.Mul(0.5)
	}
	return []mgl64.Vec3{s}
}

// CanAddPassenger implements Mount: one rider; skeleton and zombie horses
// also carry their skeleton and zombie riders.
func (h *Equine) CanAddPassenger(m *javamob.Mob, rider world.Entity) bool {
	if len(m.Passengers()) >= 1 {
		return false
	}
	if _, ok := rider.(*player.Player); ok {
		return true
	}
	switch h.kind {
	case kindSkeletonHorse:
		return entityID(rider) == "minecraft:skeleton"
	case kindZombieHorse:
		return entityID(rider) == "minecraft:zombie"
	}
	return false
}

// Controls implements Mount: getControllingPassenger is the first passenger
// when it is a player and the horse is saddled (a llama is never steered).
func (h *Equine) Controls(m *javamob.Mob, rider world.Entity) bool {
	if h.kind == kindLlama {
		return false
	}
	_, isPlayer := rider.(*player.Player)
	return isPlayer && h.saddled()
}

// RiderJump implements MountJumper: AbstractHorse.onPlayerJump and
// handleStartJump. The rider's client moves the horse, so the server only
// rears (standIfPossible) and plays the jump sound.
func (h *Equine) RiderJump(m *javamob.Mob, rider world.Entity, strength float64) {
	if !h.saddled() || h.kind == kindLlama {
		return
	}
	amount := int(strength * 100)
	if amount > 0 {
		h.allowStandSliding = true
		h.standIfPossible(m)
		m.PlaySound("jump")
	}
	h.jumpPending = PlayerJumpPendingScale(max(amount, 0))
}

// JumpStrength implements MountJumpStrength: the jump bar of a saddled
// horse.
func (h *Equine) JumpStrength(m *javamob.Mob) float64 {
	if h.kind == kindLlama {
		return 0
	}
	return m.Attr(javamob.AttrJumpStrength)
}

// RiderAdded implements MountRiderListener: AbstractHorse.addPassenger.
func (h *Equine) RiderAdded(m *javamob.Mob, rider world.Entity) {
	h.setEating(m, false)
}

// RiderRemoved implements MountRiderListener.
func (h *Equine) RiderRemoved(m *javamob.Mob, rider *world.EntityHandle) {
	h.gallopSoundCounter = 0
}

// DismountPosition implements MountDismounter:
// AbstractHorse.getDismountLocationForPassenger: beside the horse on the
// rider's main hand side (right), else the other side, else where it is.
func (h *Equine) DismountPosition(m *javamob.Mob, rider world.Entity) (mgl64.Vec3, bool) {
	for _, side := range []float64{90, -90} {
		dir := horizontalEscape(m.Width(), 0.6, m.YRot()+side)
		if pos, ok := h.dismountIn(m, dir); ok {
			return pos, true
		}
	}
	return m.Position(), true
}

// horizontalEscape is Entity.getCollisionHorizontalEscapeVector.
func horizontalEscape(vehicleWidth, riderWidth, yaw float64) mgl64.Vec3 {
	d := (vehicleWidth + riderWidth + float64(float32(1e-5))) / 2
	s := -float64(sinDeg32(yaw))
	c := float64(cosDeg32(yaw))
	mx := math.Max(math.Abs(s), math.Abs(c))
	return mgl64.Vec3{s * d / mx, 0, c * d / mx}
}

// dismountIn is getDismountLocationInDirection for a standing player
// (0.6 x 1.8): the first block column floor from the horse's feet up to 0.75
// above its top with room for the player.
func (h *Equine) dismountIn(m *javamob.Mob, dir mgl64.Vec3) (mgl64.Vec3, bool) {
	tx := m.Tx()
	x, z := m.Position()[0]+dir[0], m.Position()[2]+dir[2]
	limit := m.BBox().Max()[1] + 0.75
	pos := cube.PosFromVec3(mgl64.Vec3{x, m.BBox().Min()[1], z})
	for float64(pos[1]) < limit {
		floor := blockFloorHeight(tx, pos)
		if float64(pos[1])+floor > limit {
			break
		}
		if floor < math.Inf(1) && floor >= 0 && floor < 1 || floor == 0 {
			at := mgl64.Vec3{x, float64(pos[1]) + floor, z}
			box := cube.Box(at[0]-0.3, at[1], at[2]-0.3, at[0]+0.3, at[1]+1.8, at[2]+0.3)
			if !path.Collides(tx, box) {
				return at, true
			}
		}
		pos = pos.Side(cube.FaceUp)
	}
	return mgl64.Vec3{}, false
}

// blockFloorHeight is BlockGetter.getBlockFloorHeight: the top of the block
// below's collision when pos itself is empty, as a height relative to pos
// (DismountHelper.isBlockFloorValid wants it finite and below 1).
func blockFloorHeight(tx *world.Tx, pos cube.Pos) float64 {
	below := pos.Side(cube.FaceDown)
	b := tx.Block(below)
	top := math.Inf(-1)
	for _, bb := range b.Model().BBox(below, tx) {
		top = math.Max(top, bb.Max()[1])
	}
	if math.IsInf(top, -1) {
		return math.Inf(1)
	}
	return top - 1
}

// ---- Inventory window (saddle, armour, chest) ----

// Window implements MountWindow: AbstractHorse.openCustomInventoryScreen
// for a tamed mount that nobody else rides.
func (h *Equine) Window(m *javamob.Mob, viewer world.Entity, layout entity.WindowLayout) (entity.EntityWindow, bool) {
	if !h.Tame {
		return entity.EntityWindow{}, false
	}
	if ps := m.PassengerHandles(); len(ps) > 0 && !slices.Contains(ps, viewer.H()) {
		return entity.EntityWindow{}, false
	}
	if h.inv == nil {
		h.createInventory()
	}
	chest := h.inventoryColumns() * 3
	saddleSlot := entity.EquipSlot{Accepted: []string{"minecraft:saddle"}, Icon: "minecraft:saddle", Slot: 0}
	if h.kind == kindLlama {
		saddleSlot = entity.EquipSlot{}
	}
	var armourSlot entity.EquipSlot
	switch h.kind {
	case kindHorse, kindZombieHorse:
		armourSlot = entity.EquipSlot{Accepted: []string{"minecraft:leather_horse_armor", "minecraft:copper_horse_armor",
			"minecraft:iron_horse_armor", "minecraft:golden_horse_armor", "minecraft:diamond_horse_armor",
			"minecraft:netherite_horse_armor"}, Icon: "minecraft:iron_horse_armor", Slot: 1}
	case kindLlama:
		armourSlot = entity.EquipSlot{Accepted: carpetNames(), Icon: "minecraft:white_carpet", Slot: 1}
	}
	equip := []entity.EquipSlot{saddleSlot, armourSlot}
	win := inventory.New(2+chest, nil)
	_ = win.SetItem(0, h.Saddle)
	_ = win.SetItem(1, m.BodyArmour())
	for i := 0; i < chest; i++ {
		it, _ := h.inv.Item(i)
		_ = win.SetItem(2+i, it)
	}
	tx := m.Tx()
	handle := m.H()
	win.SlotFunc(func(i int, _, after item.Stack) {
		e, ok := handle.Entity(tx)
		if !ok {
			return
		}
		jm, ok := javamob.Of(e)
		if !ok {
			return
		}
		switch {
		case i == 0:
			if h.kind != kindLlama {
				h.setSaddle(jm, after)
			}
		case i == 1:
			jm.SetBodyArmour(after)
		case i-2 < chest:
			_ = h.inv.SetItem(i-2, after)
		}
	})
	return entity.EntityWindow{Inventory: win, Equip: equip, ChestSlots: chest}, true
}

func carpetNames() []string {
	out := make([]string, 0, 16)
	for _, c := range item.Colours() {
		out = append(out, "minecraft:"+c.String()+"_carpet")
	}
	return out
}

// DropCustomDeathLoot is AbstractHorse.dropEquipment: the saddle, the
// chest and what is in it (the body armour drops with the equipment).
func (h *Equine) DropCustomDeathLoot(m *javamob.Mob, src world.DamageSource, byPlayer bool) {
	var drops []item.Stack
	if !h.Saddle.Empty() {
		drops = append(drops, h.Saddle)
	}
	if h.Chest {
		drops = append(drops, item.NewStack(block.Chest{}, 1))
		if h.inv != nil {
			for _, it := range h.inv.Items() {
				if !it.Empty() {
					drops = append(drops, it)
				}
			}
		}
	}
	for _, s := range drops {
		m.Tx().AddEntity(m.Tx().World().EntityRegistry().Config().Item(world.EntitySpawnOpts{Position: m.Position()}, s))
	}
}
