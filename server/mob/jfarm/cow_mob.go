//go:build crossfly_full

package jfarm

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/janimal"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

func init() {
	javamob.Register(javamob.Type{
		ID: "minecraft:cow", Category: javamob.CategoryCreature, Width: 0.9, Height: 1.4, EyeHeight: 1.3,
		Attributes: cowAttributes, Families: []string{"cow", "mob"},
		New: func() javamob.Behaviour { return newCow() },
	})
	javamob.Register(javamob.Type{
		ID: "minecraft:mooshroom", Category: javamob.CategoryCreature, Width: 0.9, Height: 1.4, EyeHeight: 1.3,
		Attributes: cowAttributes, Families: []string{"mooshroom", "mob"},
		New: func() javamob.Behaviour { return newMooshroom() },
	})
}

// cowAttributes is AbstractCow.createAttributes: 10 health, speed 0.2, tempt range 10.
func cowAttributes() map[javamob.Attribute]float64 {
	a := janimal.Attributes()
	a[javamob.AttrMaxHealth], a[javamob.AttrMovementSpeed] = 10, 0.2
	return a
}

// AbstractCow is net.minecraft.world.entity.animal.cow.AbstractCow.
type AbstractCow struct {
	janimal.Base
}

// RegisterGoals is AbstractCow.registerGoals.
func (c *AbstractCow) RegisterGoals(m *javamob.Mob) {
	m.Goals().Add(0, goal.NewFloat(m))
	m.Goals().Add(1, janimal.Goal(&janimal.PanicGoal[*janimal.Body]{Speed: 2}))
	m.Goals().Add(2, janimal.Goal(&janimal.BreedGoal[*janimal.Body]{Speed: 1}))
	m.Goals().Add(3, janimal.Goal(janimal.NewTemptGoal[*janimal.Body](1.25, janimal.StackIn(janimal.CowFood), false)))
	m.Goals().Add(4, janimal.Goal(&janimal.FollowParentGoal[*janimal.Body]{Speed: 1.25}))
	m.Goals().Add(5, goal.NewWaterAvoidingRandomStroll(1, 0.001))
	m.Goals().Add(6, goal.NewLookAtPlayer(6))
	m.Goals().Add(7, goal.NewRandomLookAround())
}

// MilkCow is AbstractCow.mobInteract's bucket: an adult fills an empty bucket with milk (cow.milk sound for
// the player).
func MilkCow(m *javamob.Mob, user item.User, held item.Stack) (item.Stack, bool) {
	if b, ok := held.Item().(item.Bucket); !ok || !b.Empty() || m.Baby() {
		return held, false
	}
	m.Tx().PlaySound(m.Position(), sound.Custom{Name: "mob.cow.milk", Volume: 1, Pitch: 1})
	return janimal.FilledResult(user, held, item.NewStack(item.Bucket{Content: item.MilkBucketContent()}, 1)), true
}

// Cow is net.minecraft.world.entity.animal.cow.Cow: variants by climate and a sound variant.
type Cow struct {
	AbstractCow
	climate Climate
	sound   string
}

func newCow() *Cow {
	c := &Cow{climate: Temperate, sound: "classic"}
	c.Food = janimal.StackIn(janimal.CowFood)
	return c
}

// MobInteract is AbstractCow.mobInteract then Animal.mobInteract.
func (c *Cow) MobInteract(m *javamob.Mob, user item.User, held item.Stack) (item.Stack, bool) {
	if h, ok := MilkCow(m, user, held); ok {
		return h, true
	}
	return c.Base.MobInteract(m, user, held)
}

// FinalizeSpawn is Cow.finalizeSpawn: the variant of the biome and a random sound variant.
func (c *Cow) FinalizeSpawn(m *javamob.Mob, _ javamob.SpawnReason) {
	c.climate = FarmAnimalClimate(m.Tx().Biome(blockPos(m)))
	c.sound = pick(m, "classic", "moody")
	c.show(m)
}

// BreedOffspring is Cow.getBreedOffspring: a calf with either parent's variant.
func (c *Cow) BreedOffspring(self, partner *janimal.Body) (string, func(*javamob.Mob), bool) {
	v := c.climate
	if p, ok := partner.B.(*Cow); ok && self.Rand().IntN(2) == 0 {
		v = p.climate
	}
	return "minecraft:cow", func(baby *javamob.Mob) {
		if b, ok := baby.Behaviour().(*Cow); ok {
			b.climate = v
		}
	}, true
}

func (c *Cow) show(m *javamob.Mob) {
	_ = m.SetProperty("minecraft:climate_variant", string(c.climate))
	_ = m.SetProperty("minecraft:sound_variant", bedrockSound(c.sound))
}

// EncodeNBT is Cow.addAdditionalSaveData.
func (c *Cow) EncodeNBT(data map[string]any) {
	c.Base.EncodeNBT(data)
	data["variant"] = "minecraft:" + string(c.climate)
	data["sound_variant"] = "minecraft:" + c.sound
}

// DecodeNBT is Cow.readAdditionalSaveData.
func (c *Cow) DecodeNBT(m *javamob.Mob, data map[string]any) {
	c.Base.DecodeNBT(m, data)
	if v, ok := data["variant"].(string); ok {
		c.climate = Climate(trimNS(v))
	}
	if v, ok := data["sound_variant"].(string); ok {
		c.sound = trimNS(v)
	}
	if m != nil {
		c.show(m)
	}
}

var _ world.Entity = (*javamob.Mob)(nil)
