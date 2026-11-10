//go:build crossfly_full

package jfarm

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/janimal"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
)

func init() {
	javamob.Register(javamob.Type{
		ID: "minecraft:pig", Category: javamob.CategoryCreature, Width: 0.9, Height: 0.9, EyeHeight: 0.765,
		Attributes: func() map[javamob.Attribute]float64 {
			a := janimal.Attributes()
			a[javamob.AttrMaxHealth], a[javamob.AttrMovementSpeed] = 10, 0.25
			return a
		},
		Families: []string{"pig", "mob"},
		New: func() javamob.Behaviour {
			p := &Pig{climate: Temperate, sound: "classic"}
			p.Food = janimal.StackIn(janimal.PigFood)
			return p
		},
	})
}

// Pig is net.minecraft.world.entity.animal.pig.Pig. Riding it (controlling passenger, tickRidden,
// getRiddenSpeed with the carrot on a stick boost) waits for the base's ridden travel hooks; Steering and
// PigRiddenSpeed hold the rules.
type Pig struct {
	janimal.Base
	climate  Climate
	sound    string
	saddled  bool
	Steering Steering
}

// RegisterGoals is Pig.registerGoals.
func (p *Pig) RegisterGoals(m *javamob.Mob) {
	m.Goals().Add(0, goal.NewFloat(m))
	m.Goals().Add(1, janimal.Goal(&janimal.PanicGoal[*janimal.Body]{Speed: 1.25}))
	m.Goals().Add(3, janimal.Goal(&janimal.BreedGoal[*janimal.Body]{Speed: 1}))
	m.Goals().Add(4, janimal.Goal(janimal.NewTemptGoal[*janimal.Body](1.2, isCarrotOnAStick, false)))
	m.Goals().Add(4, janimal.Goal(janimal.NewTemptGoal[*janimal.Body](1.2, janimal.StackIn(janimal.PigFood), false)))
	m.Goals().Add(5, janimal.Goal(&janimal.FollowParentGoal[*janimal.Body]{Speed: 1.1}))
	m.Goals().Add(6, goal.NewWaterAvoidingRandomStroll(1, 0.001))
	m.Goals().Add(7, goal.NewLookAtPlayer(6))
	m.Goals().Add(8, goal.NewRandomLookAround())
}

func isCarrotOnAStick(s item.Stack) bool {
	_, ok := s.Item().(item.CarrotOnAStick)
	return ok
}

// MobInteract is Pig.mobInteract: a saddled pig without a rider is mounted by a player not holding its food
// (and not sneaking); otherwise Animal.mobInteract, then a saddle is put on an adult (the item's
// interactLivingEntity).
func (p *Pig) MobInteract(m *javamob.Mob, user item.User, held item.Stack) (item.Stack, bool) {
	food := p.IsFood(held)
	if !food && p.saddled && m.RiderCount() == 0 && !sneaking(user) {
		return held, mount(m, user)
	}
	if h, ok := p.Base.MobInteract(m, user, held); ok {
		return h, true
	}
	if _, ok := held.Item().(item.Saddle); ok && !p.saddled && !m.Baby() && !m.Dead() {
		p.saddled = true
		m.UpdateMeta(func(meta *mob.Meta) { meta.Saddled = true })
		playSound(m, "mob.pig.saddle")
		if janimal.Creative(user) {
			return held, true
		}
		return held.Grow(-1), true
	}
	return held, false
}

// FinalizeSpawn is Pig.finalizeSpawn.
func (p *Pig) FinalizeSpawn(m *javamob.Mob, _ javamob.SpawnReason) {
	p.climate = FarmAnimalClimate(m.Tx().Biome(blockPos(m)))
	p.sound = pick(m, "big", "classic", "mini")
	p.show(m)
}

// BreedOffspring is Pig.getBreedOffspring.
func (p *Pig) BreedOffspring(self, partner *janimal.Body) (string, func(*javamob.Mob), bool) {
	v := p.climate
	if o, ok := partner.B.(*Pig); ok && self.Rand().IntN(2) == 0 {
		v = o.climate
	}
	return "minecraft:pig", func(baby *javamob.Mob) {
		if b, ok := baby.Behaviour().(*Pig); ok {
			b.climate = v
			b.show(baby)
		}
	}, true
}

func (p *Pig) show(m *javamob.Mob) {
	_ = m.SetProperty("minecraft:climate_variant", string(p.climate))
	_ = m.SetProperty("minecraft:sound_variant", bedrockSound(p.sound))
	m.UpdateMeta(func(meta *mob.Meta) { meta.Saddled = p.saddled })
}

// EncodeNBT is Pig.addAdditionalSaveData (the saddle is equipment in Java; saved here as Saddle).
func (p *Pig) EncodeNBT(data map[string]any) {
	p.Base.EncodeNBT(data)
	data["variant"] = "minecraft:" + string(p.climate)
	data["sound_variant"] = "minecraft:" + p.sound
	data["Saddle"] = boolByte(p.saddled)
}

// DecodeNBT is Pig.readAdditionalSaveData.
func (p *Pig) DecodeNBT(m *javamob.Mob, data map[string]any) {
	p.Base.DecodeNBT(m, data)
	if v, ok := data["variant"].(string); ok {
		p.climate = Climate(trimNS(v))
	}
	if v, ok := data["sound_variant"].(string); ok {
		p.sound = trimNS(v)
	}
	p.saddled = janimal.NBTBool(data, "Saddle", false)
	if m != nil {
		p.show(m)
	}
}
