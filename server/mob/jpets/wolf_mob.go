//go:build crossfly_full

package jpets

import (
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/janimal"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
)

// Wolf is the behaviour of a wolf (Wolf.java).
type Wolf struct {
	Tamable
	Animal janimal.Animal

	// CollarColour is DATA_COLLAR_COLOR (red by default).
	CollarColour int
	// Variant and SoundVariant are the registry entries ("pale", "classic").
	Variant, SoundVariant string
	// Interested is DATA_INTERESTED_ID (BegGoal).
	Interested bool

	isWet, isShaking      bool
	shakeAnim, shakeAnimO float32

	// Anger is NeutralMob (20 to 39 seconds of persistent anger).
	Anger javamob.NeutralMob
	panic *janimal.PanicGoal[petHost]
	self  *world.EntityHandle
}

func (w *Wolf) animal() *janimal.Animal { return &w.Animal }
func (w *Wolf) panicking() bool         { return w.panic != nil && w.panic.Running() }

// panicGoal is TamableAnimalPanicGoal(speed, causes).
func (w *Wolf) panicGoal(speed float64, causes map[string]bool) javamob.Goal {
	w.panic = &janimal.PanicGoal[petHost]{Speed: speed}
	if causes != nil {
		w.panic.ShouldPanic = func(h petHost) bool { return causes[h.LastDamageType()] }
	}
	return &tamablePanic{t: &w.Tamable, g: wrapGoal(w.panic)}
}

// environmentalPanic is DamageTypeTags.PANIC_ENVIRONMENTAL_CAUSES.
var environmentalPanic = map[string]bool{
	"minecraft:cactus": true, "minecraft:freeze": true, "minecraft:hot_floor": true, "minecraft:sulfur_cube_hot": true,
	"minecraft:in_fire": true, "minecraft:lava": true, "minecraft:lightning_bolt": true, "minecraft:on_fire": true,
}

// MaxHeadYRot, MaxHeadXRot and HeadRotSpeed implement javamob.HeadLimiter:
// a sitting wolf lifts its head at most 20 degrees (Wolf.getMaxHeadXRot).
func (w *Wolf) MaxHeadYRot() float64 { return 75 }
func (w *Wolf) MaxHeadXRot() float64 {
	if w.InSittingPose {
		return 20
	}
	return 40
}
func (w *Wolf) HeadRotSpeed() float64 { return 10 }

// breed is Wolf.getBreedOffspring + spawnChildFromBreeding.
func (w *Wolf) breed(m, partner *javamob.Mob) bool {
	baby, err := javamob.Spawn(m.Tx(), "minecraft:wolf", m.Position(), javamob.SpawnBreeding, javamob.SpawnOptions{})
	if err != nil {
		return false
	}
	bw := baby.Behaviour().(*Wolf)
	w.babyLook(m, partner.Behaviour().(*Wolf), baby, bw)
	bw.Animal.SetBaby(true)
	baby.SetBaby(true)
	return true
}

func newWolf() javamob.Behaviour {
	w := &Wolf{CollarColour: defaultCollarColour, Variant: "pale", SoundVariant: "classic"}
	w.SideEffects = w.applyTamingSideEffects
	return w
}

// applyTamingSideEffects is Wolf.applyTamingSideEffects: 40 health when
// tamed (Paper: setHealth(getMaxHealth())), 8 when not.
func (w *Wolf) applyTamingSideEffects(m *javamob.Mob, tame bool) {
	if tame {
		m.Attributes().Instance(javamob.AttrMaxHealth).SetBase(wolfTameHealth)
		m.SetMaxHealth(wolfTameHealth)
		m.SetHealth(m.MaxHealth())
		return
	}
	m.Attributes().Instance(javamob.AttrMaxHealth).SetBase(wolfStartHealth)
	m.SetMaxHealth(wolfStartHealth)
}

// setCollar sets the collar colour (Meta.Color, both clients draw it on a
// tame wolf).
func (w *Wolf) setCollar(m *javamob.Mob, c int) {
	w.CollarColour = c
	m.UpdateMeta(func(meta *mob.Meta) { meta.Color = uint8(c) })
}

// setVariant shows the variant and sound variant.
func (w *Wolf) setVariant(m *javamob.Mob, v string) {
	w.Variant = v
	m.UpdateMeta(func(meta *mob.Meta) { meta.Variant = WolfVariantID(v) })
}

func (w *Wolf) setSoundVariant(m *javamob.Mob, v string) {
	w.SoundVariant = v
	_ = m.SetProperty("minecraft:sound_variant", BedrockSoundVariant(v))
}

// MobInteract is Wolf.mobInteract.
func (w *Wolf) MobInteract(m *javamob.Mob, user item.User, held item.Stack) (item.Stack, bool) {
	p, ok := user.(*player.Player)
	if !ok {
		return held, false
	}
	name := itemName(held)
	if w.Tame {
		if IsWolfFood(name) && m.Health() < m.MaxHealth() {
			// TamableAnimal.feed(player, hand, stack, 2, 2): 2 x nutrition.
			m.Heal(2*foodNutrition(name, 1), entity.FoodHealingSource{})
			m.PlaySound("eat")
			return consumeOne(user, held), true
		}
		if dye, isDye := DyeOfItem(name); isDye && isOwnedBy(m, p) {
			if dye != w.CollarColour {
				w.setCollar(m, dye)
				return consumeOne(user, held), true
			}
			return w.animalInteract(m, p, held)
		}
		if name == "minecraft:wolf_armor" && m.BodyArmour().Empty() && isOwnedBy(m, p) && !m.Baby() {
			m.SetBodyArmour(held.Grow(1 - held.Count()))
			m.PlaySound("armor")
			return consumeOne(user, held), true
		}
		if armour := m.BodyArmour(); w.InSittingPose && !armour.Empty() && isOwnedBy(m, p) &&
			armour.Durability() < armour.MaxDurability() && name == "minecraft:armadillo_scute" {
			repair := int(float32(armour.MaxDurability()) * wolfArmourRepair)
			m.SetBodyArmour(armour.WithDurability(min(armour.MaxDurability(), armour.Durability()+repair)))
			m.PlaySound("armor.repair")
			return held.Grow(-1), true // itemStack.shrink(1): also in creative
		}
		if left, used := w.animalInteract(m, p, held); used {
			return left, true
		}
		if isOwnedBy(m, p) {
			w.OrderedToSit = !w.OrderedToSit
			m.SetJumping(false)
			m.Navigation().Stop()
			m.SetTarget(nil)
			return held, true
		}
		return held, false
	}
	if name == "minecraft:bone" && !w.angry(m) {
		left := consumeOne(user, held)
		w.tryToTame(m, p)
		return left, true
	}
	return w.animalInteract(m, p, held)
}

// animalInteract is Animal.mobInteract: food for breeding or growing.
func (w *Wolf) animalInteract(m *javamob.Mob, p *player.Player, held item.Stack) (item.Stack, bool) {
	return animalFeed(m, &w.Animal, p, held, IsWolfFood(itemName(held)))
}

// tryToTame is Wolf.tryToTame: one in three tames the wolf, which sits.
func (w *Wolf) tryToTame(m *javamob.Mob, p *player.Player) {
	if m.Rand().IntN(3) == 0 {
		w.TameTo(m, p)
		m.Navigation().Stop()
		m.SetTarget(nil)
		w.OrderedToSit = true
		broadcastEvent(m, tameSucceeded{})
		return
	}
	broadcastEvent(m, tameFailed{})
}

// FilterDamage is Wolf.actuallyHurt with wolf armour: the armour takes the
// hit instead of the wolf.
func (w *Wolf) FilterDamage(m *javamob.Mob, d *mob.Damage) {
	left, absorbed, cracked := WolfArmourAbsorb(m.BodyArmour(), d.Raw, d.Source)
	if !absorbed {
		return
	}
	d.Cancel = true
	m.SetBodyArmour(left)
	if left.Empty() {
		m.PlaySound("armor.break")
	} else if cracked {
		m.PlaySound("armor.crack")
		m.Tx().AddParticle(m.Position().Add(mgl64Y(1)), particle.Splash{})
	}
}

// OnHurt is Wolf.hurtServer with Paper's order: a wolf that was hurt stops
// sitting.
func (w *Wolf) OnHurt(m *javamob.Mob, dealt float64, src world.DamageSource) {
	w.OrderedToSit = false
	w.Animal.ResetLove()
}

// PostTick is Wolf.tick after super.tick: drying and shaking.
func (w *Wolf) PostTick(m *javamob.Mob) {
	if m.Dead() {
		return
	}
	if m.InWaterOrRain() {
		w.isWet = true
		if w.isShaking {
			w.cancelShake()
		}
		return
	}
	if (w.isWet || w.isShaking) && w.isShaking {
		if w.shakeAnim == 0 {
			m.PlaySound("shake")
		}
		w.shakeAnimO = w.shakeAnim
		w.shakeAnim += 0.05
		if w.shakeAnimO >= 2 {
			w.isWet, w.isShaking = false, false
			w.shakeAnimO, w.shakeAnim = 0, 0
		}
	}
}

func (w *Wolf) cancelShake() {
	w.isShaking = false
	w.shakeAnim, w.shakeAnimO = 0, 0
}

// AiStep is Wolf.aiStep: age and love (Animal), a wet wolf on the ground
// and not walking a path shakes itself dry (entity event 8), persistent
// anger.
func (w *Wolf) AiStep(m *javamob.Mob) {
	animalTick(m, &w.Animal)
	w.Anger.UpdatePersistentAnger(m, true)
	if a := w.Anger.IsAngry(m); m.Meta().Angry != a {
		m.UpdateMeta(func(meta *mob.Meta) { meta.Angry = a })
	}
	if w.isWet && !w.isShaking && m.Navigation().Done() && m.OnGround() {
		w.isShaking = true
		w.shakeAnim, w.shakeAnimO = 0, 0
		broadcastEvent(m, shakeWetness{})
	}
}

// OnDeath is Wolf.die: no more shaking.
func (w *Wolf) OnDeath(m *javamob.Mob, src world.DamageSource) {
	w.isWet, w.isShaking = false, false
	w.shakeAnim, w.shakeAnimO = 0, 0
}

// CanAttack is TamableAnimal.canAttack: never the owner.
func (w *Wolf) CanAttack(m *javamob.Mob, e world.Entity) bool { return !isOwnedBy(m, e) }

// FinalizeSpawn is Wolf.finalizeSpawn: the variant from the spawn conditions
// at the wolf (a pack shares its first wolf's), a random sound variant.
func (w *Wolf) FinalizeSpawn(m *javamob.Mob, reason javamob.SpawnReason) {
	w.setVariant(m, WolfSpawnVariant(m.Rand(), spawnContextAt(m)))
	w.setSoundVariant(m, RandomSoundVariant(m.Rand(), WolfSoundVariants))
	w.setCollar(m, w.CollarColour)
}

// EncodeNBT and DecodeNBT save the Java fields: CollarColor, variant,
// sound_variant, Sitting, the animal state.
func (w *Wolf) EncodeNBT(data map[string]any) {
	w.Animal.EncodeNBT(data)
	w.Anger.EncodeAnger(data)
	w.EncodeTamable(data)
	data["CollarColor"] = uint8(w.CollarColour)
	data["variant"] = "minecraft:" + w.Variant
	data["sound_variant"] = "minecraft:" + w.SoundVariant
}

func (w *Wolf) DecodeNBT(m *javamob.Mob, data map[string]any) {
	w.Animal.DecodeNBT(data)
	w.Anger.DecodeAnger(m, data)
	w.DecodeTamable(m, data)
	if c, ok := data["CollarColor"].(uint8); ok {
		w.setCollar(m, int(c))
	}
	if v, ok := data["variant"].(string); ok {
		w.setVariant(m, trimNS(v))
	}
	if v, ok := data["sound_variant"].(string); ok {
		w.setSoundVariant(m, trimNS(v))
	}
}

// WolfBabyLook is Wolf.getBreedOffspring: either parent's variant; a baby
// of a tame wolf is tame, owned by its owner, with the parents' collars
// mixed; a random sound variant.
func (w *Wolf) babyLook(m *javamob.Mob, partner *Wolf, baby *javamob.Mob, bw *Wolf) {
	if m.Rand().IntN(2) == 0 {
		bw.setVariant(baby, w.Variant)
	} else {
		bw.setVariant(baby, partner.Variant)
	}
	if w.Tame {
		baby.SetOwnerOf(m.Mob)
		bw.SetTame(baby, true, true)
		bw.setCollar(baby, MixDyes(m.Rand(), w.CollarColour, partner.CollarColour))
	}
	bw.setSoundVariant(baby, RandomSoundVariant(m.Rand(), WolfSoundVariants))
}

// wolfCanMate is Wolf.canMate: both tame, the partner not sitting, both in
// love.
func wolfCanMate(w, partner *Wolf) bool {
	return w != partner && w.Tame && partner.Tame && !partner.InSittingPose && w.Animal.InLove() && partner.Animal.InLove()
}

func trimNS(s string) string {
	if len(s) > 10 && s[:10] == "minecraft:" {
		return s[10:]
	}
	return s
}
