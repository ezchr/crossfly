//go:build crossfly_full

package jfarm

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/janimal"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

func init() {
	javamob.Register(javamob.Type{
		ID: "minecraft:chicken", Category: javamob.CategoryCreature, Width: 0.4, Height: 0.7, EyeHeight: 0.644,
		Attributes: func() map[javamob.Attribute]float64 {
			a := janimal.Attributes()
			a[javamob.AttrMaxHealth], a[javamob.AttrMovementSpeed] = 4, 0.25
			return a
		},
		Families: []string{"chicken", "mob"},
		New: func() javamob.Behaviour {
			c := &Chicken{climate: Temperate, sound: "classic"}
			c.Food = janimal.StackIn(janimal.ChickenFood)
			return c
		},
	})
}

// Chicken is net.minecraft.world.entity.animal.chicken.Chicken.
type Chicken struct {
	janimal.Base
	eggTime EggTimer
	started bool
	jockey  bool
	climate Climate
	sound   string
}

// RegisterGoals is Chicken.registerGoals (and the constructor: egg timer, WATER malus 0).
func (c *Chicken) RegisterGoals(m *javamob.Mob) {
	if !c.started {
		c.eggTime, c.started = NewEggTimer(m.Rand()), true
	}
	m.SetPathfindingMalus(path.Water, 0)
	m.Goals().Add(0, goal.NewFloat(m))
	m.Goals().Add(1, janimal.Goal(&janimal.PanicGoal[*janimal.Body]{Speed: 1.4}))
	m.Goals().Add(2, janimal.Goal(&janimal.BreedGoal[*janimal.Body]{Speed: 1}))
	m.Goals().Add(3, janimal.Goal(janimal.NewTemptGoal[*janimal.Body](1, janimal.StackIn(janimal.ChickenFood), false)))
	m.Goals().Add(4, janimal.Goal(&janimal.FollowParentGoal[*janimal.Body]{Speed: 1.1}))
	m.Goals().Add(5, goal.NewWaterAvoidingRandomStroll(1, 0.001))
	m.Goals().Add(6, goal.NewLookAtPlayer(6))
	m.Goals().Add(7, goal.NewRandomLookAround())
}

// PostTick is the end of Chicken.aiStep: the slow fall and the egg timer, after the Animal tick.
func (c *Chicken) PostTick(m *javamob.Mob) {
	c.Base.PostTick(m)
	if v := m.Velocity(); !m.OnGround() && v[1] < 0 {
		m.SetVelocity(mgl64.Vec3{v[0], v[1] * ChickenFallFactor, v[2]})
	}
	if c.eggTime.Tick(m.Rand(), !m.Dead(), m.Baby(), c.jockey) {
		if it, ok := javaItem(EggItem(c.climate)); ok {
			m.SpawnAtLocation(item.NewStack(it, 1))
			m.Tx().PlaySound(m.Position(), sound.Custom{Name: "mob.chicken.plop", Volume: 1, Pitch: EggPitch(m.Rand())})
			gameEvent(m, "minecraft:entity_place")
		}
	}
}

// XPReward is Chicken.getBaseExperienceReward.
func (c *Chicken) XPReward(m *javamob.Mob, base int) int {
	if c.jockey {
		return ChickenJockeyXP
	}
	return c.Base.XPReward(m, base)
}

// SetBaby is the chicken's BABY_DIMENSIONS (0.3 x 0.4).
func (c *Chicken) SetBaby(m *javamob.Mob, baby bool) {
	if baby {
		m.SetDimensions(0.3, 0.4)
	}
}

// RemoveWhenFarAway is Chicken.removeWhenFarAway: only chicken jockeys despawn.
func (c *Chicken) RemoveWhenFarAway(*javamob.Mob, float64) bool { return c.jockey }

// SetChickenJockey is Chicken.setChickenJockey (the zombie family makes jockeys).
func (c *Chicken) SetChickenJockey(v bool) { c.jockey = v }

// FinalizeSpawn is Chicken.finalizeSpawn.
func (c *Chicken) FinalizeSpawn(m *javamob.Mob, _ javamob.SpawnReason) {
	c.climate = FarmAnimalClimate(m.Tx().Biome(blockPos(m)))
	c.sound = pick(m, "classic", "picky")
	c.show(m)
}

// BreedOffspring is Chicken.getBreedOffspring: either parent's variant.
func (c *Chicken) BreedOffspring(self, partner *janimal.Body) (string, func(*javamob.Mob), bool) {
	v := c.climate
	if p, ok := partner.B.(*Chicken); ok && self.Rand().IntN(2) == 0 {
		v = p.climate
	}
	return "minecraft:chicken", func(baby *javamob.Mob) {
		if b, ok := baby.Behaviour().(*Chicken); ok {
			b.climate = v
			b.show(baby)
		}
	}, true
}

func (c *Chicken) show(m *javamob.Mob) {
	_ = m.SetProperty("minecraft:climate_variant", string(c.climate))
	_ = m.SetProperty("minecraft:sound_variant", bedrockSound(c.sound))
}

// EncodeNBT is Chicken.addAdditionalSaveData.
func (c *Chicken) EncodeNBT(data map[string]any) {
	c.Base.EncodeNBT(data)
	data["IsChickenJockey"] = boolByte(c.jockey)
	data["EggLayTime"] = int32(c.eggTime)
	data["variant"] = "minecraft:" + string(c.climate)
	data["sound_variant"] = "minecraft:" + c.sound
}

// DecodeNBT is Chicken.readAdditionalSaveData.
func (c *Chicken) DecodeNBT(m *javamob.Mob, data map[string]any) {
	c.Base.DecodeNBT(m, data)
	c.jockey = janimal.NBTBool(data, "IsChickenJockey", false)
	if _, ok := data["EggLayTime"]; ok {
		c.eggTime, c.started = EggTimer(janimal.NBTInt(data, "EggLayTime", 6000)), true
	}
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
