//go:build crossfly_full

package jpets

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/janimal"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// Cats (animal/feline/Cat.java).

// IsCatFood is ItemTags.CAT_FOOD (and OCELOT_FOOD): raw cod and salmon.
func IsCatFood(name string) bool { return name == "minecraft:cod" || name == "minecraft:salmon" }

// Cat is the behaviour of a cat.
type Cat struct {
	Tamable
	Animal janimal.Animal

	Variant, SoundVariant string
	CollarColour          int
	lying, relaxStateOne  bool
	// avoidPlayers is the CatAvoidEntityGoal a wild cat runs.
	avoidPlayers javamob.Goal
	tempt        *catTempt
	panic        *janimal.PanicGoal[petHost]
}

func newCat() javamob.Behaviour {
	return &Cat{Variant: "black", SoundVariant: "classic", CollarColour: defaultCollarColour}
}

func (c *Cat) setVariant(m *javamob.Mob, v string) {
	c.Variant = v
	m.UpdateMeta(func(meta *mob.Meta) { meta.Variant = CatVariantID(v) })
}

func (c *Cat) setSoundVariant(m *javamob.Mob, v string) {
	c.SoundVariant = v
	_ = m.SetProperty("minecraft:sound_variant", BedrockSoundVariant(v))
}

func (c *Cat) setCollar(m *javamob.Mob, d int) {
	c.CollarColour = d
	m.UpdateMeta(func(meta *mob.Meta) { meta.Color = uint8(d) })
}

// setLying is Cat.setLying (IS_LYING), the laying down flag.
func (c *Cat) setLying(m *javamob.Mob, v bool) {
	c.lying = v
	if m.ActorFlag(protocol.EntityDataFlagLayingDown) != v {
		m.SetActorFlag(protocol.EntityDataFlagLayingDown, v)
	}
}

// setRelaxStateOne is Cat.setRelaxStateOne (RELAX_STATE_ONE), the resting
// flag.
func (c *Cat) setRelaxStateOne(m *javamob.Mob, v bool) {
	c.relaxStateOne = v
	if m.ActorFlag(protocol.EntityDataFlagResting) != v {
		m.SetActorFlag(protocol.EntityDataFlagResting, v)
	}
}

// SetTame is Cat.setTame: also reassessTameGoals (a wild cat avoids
// players).
func (c *Cat) setTame(m *javamob.Mob, tame, sideEffects bool) {
	c.Tamable.SetTame(m, tame, sideEffects)
	c.reassessTameGoals(m)
}

// reassessTameGoals is Cat.reassessTameGoals: the avoid players goal (16
// blocks, 0.8/1.33) at priority 4 only while wild.
func (c *Cat) reassessTameGoals(m *javamob.Mob) {
	if c.avoidPlayers == nil {
		return
	}
	m.Goals().Remove(m, c.avoidPlayers)
	if !c.Tame {
		m.Goals().Add(4, c.avoidPlayers)
	}
}

// MobInteract is Cat.mobInteract.
func (c *Cat) MobInteract(m *javamob.Mob, user item.User, held item.Stack) (item.Stack, bool) {
	p, ok := user.(*player.Player)
	if !ok {
		return held, false
	}
	name := itemName(held)
	if c.Tame {
		if !isOwnedBy(m, p) {
			return c.animalInteract(m, p, held)
		}
		if dye, isDye := DyeOfItem(name); isDye {
			if dye != c.CollarColour {
				c.setCollar(m, dye)
				m.SetPersistenceRequired()
				return consumeOne(user, held), true
			}
		} else if IsCatFood(name) && m.Health() < m.MaxHealth() {
			// TamableAnimal.feed(player, hand, stack, 1, 1).
			m.Heal(foodNutrition(name, 1), entity.FoodHealingSource{})
			m.PlaySound("eat")
			return consumeOne(user, held), true
		}
		if left, used := c.animalInteract(m, p, held); used {
			return left, true
		}
		c.OrderedToSit = !c.OrderedToSit
		return held, true
	}
	if IsCatFood(name) {
		left := consumeOne(user, held)
		c.tryToTame(m, p)
		m.SetPersistenceRequired()
		m.PlaySound("eat")
		return left, true
	}
	left, used := c.animalInteract(m, p, held)
	if used {
		m.SetPersistenceRequired()
	}
	return left, used
}

func (c *Cat) animalInteract(m *javamob.Mob, p *player.Player, held item.Stack) (item.Stack, bool) {
	return animalFeed(m, &c.Animal, p, held, IsCatFood(itemName(held)))
}

// tryToTame is Cat.tryToTame: one in three, and the cat sits.
func (c *Cat) tryToTame(m *javamob.Mob, p *player.Player) {
	if m.Rand().IntN(3) == 0 {
		c.TameTo(m, p)
		c.reassessTameGoals(m)
		c.OrderedToSit = true
		broadcastEvent(m, tameSucceeded{})
		return
	}
	broadcastEvent(m, tameFailed{})
}

// RemoveWhenFarAway is Cat.removeWhenFarAway: a wild cat older than two
// minutes.
func (c *Cat) RemoveWhenFarAway(m *javamob.Mob, distSqr float64) bool {
	return !c.Tame && m.TickCount() > 2400
}

// CustomServerAiStep is Cat.customServerAiStep: crouch while tempted (speed
// 0.6), sprint while fleeing (1.33).
func (c *Cat) CustomServerAiStep(m *javamob.Mob) {
	sneak, sprint := false, false
	if mc := m.MoveControl(); mc.HasWanted() {
		switch mc.SpeedModifier() {
		case 0.6:
			sneak = true
		case 1.33:
			sprint = true
		}
	}
	m.UpdateMeta(func(meta *mob.Meta) { meta.Sneaking, meta.Sprinting = sneak, sprint })
}

// PostTick is Cat.tick: beg sound every 100 ticks while a wild cat is
// tempted, purring while lying.
func (c *Cat) PostTick(m *javamob.Mob) {
	if c.tempt != nil && c.tempt.Running() && !c.Tame && m.TickCount()%100 == 0 {
		m.PlaySound("beg")
	}
	if (c.lying || c.relaxStateOne) && m.TickCount()%5 == 0 {
		m.PlaySound("purr")
	}
}

// OnHurt resets love (Animal.hurtServer).
func (c *Cat) OnHurt(m *javamob.Mob, dealt float64, src world.DamageSource) { c.Animal.ResetLove() }

// FinalizeSpawn is Cat.finalizeSpawn.
func (c *Cat) FinalizeSpawn(m *javamob.Mob, reason javamob.SpawnReason) {
	c.setVariant(m, CatSpawnVariant(m.Rand(), spawnContextAt(m)))
	c.setSoundVariant(m, RandomSoundVariant(m.Rand(), CatSoundVariants))
	c.setCollar(m, c.CollarColour)
}

// CanAttack is TamableAnimal.canAttack.
func (c *Cat) CanAttack(m *javamob.Mob, e world.Entity) bool { return !isOwnedBy(m, e) }

// EncodeNBT and DecodeNBT: variant, sound_variant, CollarColor, Sitting.
func (c *Cat) EncodeNBT(data map[string]any) {
	c.Animal.EncodeNBT(data)
	c.EncodeTamable(data)
	data["variant"] = "minecraft:" + c.Variant
	data["sound_variant"] = "minecraft:" + c.SoundVariant
	data["CollarColor"] = uint8(c.CollarColour)
}

func (c *Cat) DecodeNBT(m *javamob.Mob, data map[string]any) {
	c.Animal.DecodeNBT(data)
	c.DecodeTamable(m, data)
	c.reassessTameGoals(m)
	if v, ok := data["variant"].(string); ok {
		c.setVariant(m, trimNS(v))
	}
	if v, ok := data["sound_variant"].(string); ok {
		c.setSoundVariant(m, trimNS(v))
	}
	if v, ok := data["CollarColor"].(uint8); ok {
		c.setCollar(m, int(v))
	}
}

// babyLook is Cat.getBreedOffspring.
func (c *Cat) babyLook(m *javamob.Mob, partner *Cat, baby *javamob.Mob, bc *Cat) {
	if m.Rand().IntN(2) == 0 {
		bc.setVariant(baby, c.Variant)
	} else {
		bc.setVariant(baby, partner.Variant)
	}
	if c.Tame {
		baby.SetOwnerOf(m.Mob)
		bc.setTame(baby, true, true)
		bc.setCollar(baby, MixDyes(m.Rand(), c.CollarColour, partner.CollarColour))
	}
}

// catCanMate is Cat.canMate: both tame and in love.
func catCanMate(c, partner *Cat) bool {
	return c != partner && c.Tame && partner.Tame && c.Animal.InLove() && partner.Animal.InLove()
}

// ---- goals ----

// catRelaxOnOwner is Cat.CatRelaxOnOwnerGoal (no flags): a tame cat lies down
// on the bed of its sleeping owner within 10 blocks; when the owner wakes
// after sleeping 100 ticks it may leave a morning gift.
type catRelaxOnOwner struct {
	c       *Cat
	owner   *world.EntityHandle
	goalPos cube.Pos
	hasGoal bool
	onBed   int
	// sleepTicks counts the owner's sleep while the goal runs (getSleepTimer).
	sleepTicks int
}

func (g *catRelaxOnOwner) Flags() javamob.Flags { return 0 }

func (g *catRelaxOnOwner) CanUse(m *javamob.Mob) bool {
	if !g.c.Tame || g.c.OrderedToSit {
		return false
	}
	o, ok := ownerOf(m)
	if !ok {
		return false
	}
	p, ok := o.(*player.Player)
	if !ok {
		return false
	}
	g.owner = p.H()
	if !sleeping(p) || distSq(m.Position(), p.Position()) > 100 {
		return false
	}
	pos := cube.PosFromVec3(p.Position())
	bed, ok := m.Tx().Block(pos).(block.Bed)
	if !ok {
		return false
	}
	// ownerPos.relative(FACING.getOpposite()): FACING points from the foot
	// to the head, so from the head block this is the foot.
	g.goalPos, g.hasGoal = pos.Side(bed.Facing.Opposite().Face()), true
	return !g.spaceOccupied(m)
}

// spaceOccupied: another cat lies or relaxes within 2 blocks of the spot.
func (g *catRelaxOnOwner) spaceOccupied(m *javamob.Mob) bool {
	box := cube.Box(float64(g.goalPos[0]), float64(g.goalPos[1]), float64(g.goalPos[2]),
		float64(g.goalPos[0]+1), float64(g.goalPos[1]+1), float64(g.goalPos[2]+1)).Grow(2)
	for e := range m.Tx().EntitiesWithin(box) {
		if e.H() == m.H() {
			continue
		}
		if o, ok := javamob.Of(e); ok {
			if oc, ok := o.Behaviour().(*Cat); ok && (oc.lying || oc.relaxStateOne) {
				return true
			}
		}
	}
	return false
}

func (g *catRelaxOnOwner) CanContinueToUse(m *javamob.Mob) bool {
	if !g.c.Tame || g.c.OrderedToSit || g.owner == nil || !g.hasGoal {
		return false
	}
	o, ok := g.owner.Entity(m.Tx())
	p, isPlayer := o.(*player.Player)
	return ok && isPlayer && sleeping(p) && !g.spaceOccupied(m)
}

func (g *catRelaxOnOwner) Start(m *javamob.Mob) {
	if g.hasGoal {
		g.c.SetInSittingPose(m, false)
		m.Navigation().MoveTo(m, posVec(g.goalPos), 0, f32(1.1))
	}
}

func (g *catRelaxOnOwner) Stop(m *javamob.Mob) {
	g.c.setLying(m, false)
	if o, ok := g.owner.Entity(m.Tx()); ok {
		if _, ok := o.(*player.Player); ok && g.sleepTicks >= 100 && m.Rand().Float32() < catGiftChance(m.Tx()) {
			g.giveMorningGift(m)
		}
	}
	g.onBed, g.sleepTicks = 0, 0
	g.c.setRelaxStateOne(m, false)
	m.Navigation().Stop()
}

func (g *catRelaxOnOwner) Tick(m *javamob.Mob) {
	o, ok := g.owner.Entity(m.Tx())
	if !ok || !g.hasGoal {
		return
	}
	g.sleepTicks += 2 // ticked every other tick
	g.c.SetInSittingPose(m, false)
	m.Navigation().MoveTo(m, posVec(g.goalPos), 0, f32(1.1))
	if distSq(m.Position(), o.Position()) < 2.5 {
		g.onBed++
		if g.onBed > javamob.ReducedTickDelay(16) {
			g.c.setLying(m, true)
			g.c.setRelaxStateOne(m, false)
		} else {
			m.LookAtEntity(o, 45, 45)
			g.c.setRelaxStateOne(m, true)
		}
		return
	}
	g.c.setLying(m, false)
}

// giveMorningGift is CatRelaxOnOwnerGoal.giveMorningGift: the cat hops up to
// 5 blocks away and drops one item of gameplay/cat_morning_gift in front of
// it.
func (g *catRelaxOnOwner) giveMorningGift(m *javamob.Mob) {
	r := m.Rand()
	at := cube.PosFromVec3(m.Position())
	if h, ok := core.LeashHolder(m.Mob); ok {
		at = cube.PosFromVec3(h.Position())
	}
	randomTeleport(m, mgl64.Vec3{float64(at[0] + r.IntN(11) - 5), float64(at[1] + r.IntN(5) - 2), float64(at[2] + r.IntN(11) - 5)})
	pos := cube.PosFromVec3(m.Position())
	s := catGift(r)
	drop := mgl64.Vec3{float64(pos[0]) - float64(sinDeg32(m.BodyYRot())), float64(pos[1]), float64(pos[2]) + float64(cosDeg32(m.BodyYRot()))}
	m.Tx().AddEntity(m.Tx().World().EntityRegistry().Config().Item(world.EntitySpawnOpts{Position: drop}, s))
}

// catGift is the loot table gameplay/cat_morning_gift: rabbit hide, rabbit
// foot, raw chicken, feather, rotten flesh, string (weight 10 each) or
// phantom membrane (2).
func catGift(r *rand.Rand) item.Stack {
	n := r.IntN(62)
	names := []string{"minecraft:rabbit_hide", "minecraft:rabbit_foot", "minecraft:chicken", "minecraft:feather",
		"minecraft:rotten_flesh", "minecraft:string"}
	name := "minecraft:phantom_membrane"
	if n < 60 {
		name = names[n/10]
	}
	it, ok := world.ItemByName(name, 0)
	if !ok {
		return item.Stack{}
	}
	return item.NewStack(it, 1)
}

// catGiftChance is the environment attribute
// minecraft:gameplay/cat_waking_up_gift_chance of the day timeline: 0.7
// from 23667 to 362 ticks into the day, 0 otherwise.
func catGiftChance(tx *world.Tx) float32 {
	t := tx.World().Time() % 24000
	if t < 0 {
		t += 24000
	}
	if t >= 23667 || t < 362 {
		return 0.7
	}
	return 0
}

// sleeping reports whether p sleeps in a bed.
func sleeping(p *player.Player) bool {
	_, ok := p.Sleeping()
	return ok
}

func posVec(p cube.Pos) mgl64.Vec3 { return mgl64.Vec3{float64(p[0]), float64(p[1]), float64(p[2])} }
