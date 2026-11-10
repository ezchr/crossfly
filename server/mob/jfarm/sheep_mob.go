//go:build crossfly_full

package jfarm

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/janimal"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func init() {
	javamob.Register(javamob.Type{
		ID: "minecraft:sheep", Category: javamob.CategoryCreature, Width: 0.9, Height: 1.3, EyeHeight: 1.235,
		Attributes: func() map[javamob.Attribute]float64 {
			a := janimal.Attributes()
			a[javamob.AttrMaxHealth], a[javamob.AttrMovementSpeed] = 8, 0.23
			return a
		},
		Families: []string{"sheep", "mob"},
		New: func() javamob.Behaviour {
			s := &Sheep{}
			s.Food = janimal.StackIn(janimal.SheepFood)
			return s
		},
	})
}

// Sheep is net.minecraft.world.entity.animal.sheep.Sheep.
type Sheep struct {
	janimal.Base
	wool Wool
	eat  *janimal.EatBlockGoal[*janimal.Body]
}

// RegisterGoals is Sheep.registerGoals.
func (s *Sheep) RegisterGoals(m *javamob.Mob) {
	s.eat = &janimal.EatBlockGoal[*janimal.Body]{}
	m.Goals().Add(0, goal.NewFloat(m))
	m.Goals().Add(1, janimal.Goal(&janimal.PanicGoal[*janimal.Body]{Speed: 1.25}))
	m.Goals().Add(2, janimal.Goal(&janimal.BreedGoal[*janimal.Body]{Speed: 1}))
	m.Goals().Add(3, janimal.Goal(janimal.NewTemptGoal[*janimal.Body](1.1, janimal.StackIn(janimal.SheepFood), false)))
	m.Goals().Add(4, janimal.Goal(&janimal.FollowParentGoal[*janimal.Body]{Speed: 1.1}))
	m.Goals().Add(5, janimal.Goal(s.eat))
	m.Goals().Add(6, goal.NewWaterAvoidingRandomStroll(1, 0.001))
	m.Goals().Add(7, goal.NewLookAtPlayer(6))
	m.Goals().Add(8, goal.NewRandomLookAround())
}

// Colour is Sheep.getColor (javasession and the Bedrock session read the Meta).
func (s *Sheep) Colour() item.Colour { return s.wool.Colour() }

// Sheared is Sheep.isSheared.
func (s *Sheep) Sheared() bool { return s.wool.Sheared() }

func (s *Sheep) show(m *javamob.Mob) {
	m.UpdateMeta(func(meta *mob.Meta) {
		meta.Color = s.wool.Colour().Uint8()
		meta.Sheared = s.wool.Sheared()
	})
}

// Ate is Sheep.ate: the wool grows back and a lamb grows by 60 seconds.
func (s *Sheep) Ate(b *janimal.Body) {
	s.wool = s.wool.WithSheared(false)
	if s.A.CanAgeUp() {
		if s.A.AgeUp(SheepEatGrowSeconds, false) {
			b.SetBaby(s.A.Baby())
		}
	}
	s.show(b.Mob)
}

// MobInteract is Sheep.mobInteract (shears), Animal.mobInteract (wheat), then DyeItem.interactLivingEntity.
func (s *Sheep) MobInteract(m *javamob.Mob, user item.User, held item.Stack) (item.Stack, bool) {
	if _, ok := held.Item().(item.Shears); ok {
		if !s.wool.Sheared() && !m.Baby() {
			s.Shear(m)
			return held.Damage(1), true
		}
		return held, true // InteractionResult.CONSUME
	}
	if h, ok := s.Base.MobInteract(m, user, held); ok {
		return h, true
	}
	if d, ok := held.Item().(item.Dye); ok && !m.Dead() && !s.wool.Sheared() && d.Colour != s.wool.Colour() {
		playSound(m, "item.dye.use")
		s.wool = s.wool.WithColour(d.Colour)
		s.show(m)
		if janimal.Creative(user) {
			return held, true
		}
		return held.Grow(-1), true
	}
	return held, false
}

// Shear is Sheep.shear: the shearing sound, 1-3 wool of the sheep's colour (shearing/sheep/<colour>), each
// dropped one block up with a small random push, and the sheep sheared. Dispensers call it too.
func (s *Sheep) Shear(m *javamob.Mob) {
	playSound(m, "mob.sheep.shear")
	r := m.Rand()
	n := ShearWoolCount(r)
	for range n {
		x, y, z := WoolDropMotion(r)
		spawnItemWithPush(m, item.NewStack(block.Wool{Colour: s.wool.Colour()}, 1), 1, mgl64.Vec3{x, y, z})
	}
	s.wool = s.wool.WithSheared(true)
	s.show(m)
}

// ReadyForShearing is Sheep.readyForShearing.
func (s *Sheep) ReadyForShearing(m *javamob.Mob) bool { return !s.wool.Sheared() && !m.Baby() }

// FinalizeSpawn is Sheep.finalizeSpawn: the colour of the biome's climate.
func (s *Sheep) FinalizeSpawn(m *javamob.Mob, _ javamob.SpawnReason) {
	s.wool = s.wool.WithColour(SheepSpawnColour(m.Rand(), FarmAnimalClimate(m.Tx().Biome(blockPos(m)))))
	s.show(m)
}

// BreedOffspring is Sheep.getBreedOffspring: the lamb's colour is the parents' dyes mixed.
func (s *Sheep) BreedOffspring(self, partner *janimal.Body) (string, func(*javamob.Mob), bool) {
	other := s.wool.Colour()
	if p, ok := partner.B.(*Sheep); ok {
		other = p.wool.Colour()
	}
	c := MixedColour(self.Rand(), s.wool.Colour(), other)
	return "minecraft:sheep", func(baby *javamob.Mob) {
		if b, ok := baby.Behaviour().(*Sheep); ok {
			b.wool = b.wool.WithColour(c)
			b.show(baby)
		}
	}, true
}

// EncodeNBT is Sheep.addAdditionalSaveData: Sheared and Color (the legacy byte id).
func (s *Sheep) EncodeNBT(data map[string]any) {
	s.Base.EncodeNBT(data)
	data["Sheared"] = boolByte(s.wool.Sheared())
	data["Color"] = s.wool.Colour().Uint8()
}

// DecodeNBT is Sheep.readAdditionalSaveData.
func (s *Sheep) DecodeNBT(m *javamob.Mob, data map[string]any) {
	s.Base.DecodeNBT(m, data)
	s.wool = Wool(janimal.NBTInt(data, "Color", 0) & 0x0f).WithSheared(janimal.NBTBool(data, "Sheared", false))
	if m != nil {
		s.show(m)
	}
}

var _ world.Entity = (*javamob.Mob)(nil)
