//go:build crossfly_full

package jfarm

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/janimal"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/google/uuid"
)

// Mooshroom is net.minecraft.world.entity.animal.cow.MushroomCow.
type Mooshroom struct {
	AbstractCow
	variant MooshroomVariant
	// stew is MushroomCow.stewEffects: the flower a brown mooshroom ate, for its next stew.
	stew          *StewEffect
	stewItem      string
	lastLightning uuid.UUID
}

func newMooshroom() *Mooshroom {
	c := &Mooshroom{}
	c.Food = janimal.StackIn(janimal.CowFood)
	return c
}

func (c *Mooshroom) show(m *javamob.Mob) {
	m.UpdateMeta(func(meta *mob.Meta) { meta.Variant = int32(c.variant) })
}

// MobInteract is MushroomCow.mobInteract: a bowl gives (suspicious) stew, shears turn it into a cow, a brown
// mooshroom eats a flower for its next stew; then the cow's bucket and Animal.mobInteract.
func (c *Mooshroom) MobInteract(m *javamob.Mob, user item.User, held item.Stack) (item.Stack, bool) {
	if _, ok := held.Item().(item.Bowl); ok && !m.Baby() {
		var stew item.Stack
		name := "mob.mooshroom.suspicious_milk"
		if c.stew != nil {
			stew = suspiciousStew(c.stewItem)
			c.stew, c.stewItem = nil, ""
		} else {
			stew, name = item.NewStack(item.MushroomStew{}, 1), "mob.mooshroom.milk"
		}
		playSound(m, name)
		return janimal.FilledResult(user, held, stew), true
	}
	if _, ok := held.Item().(item.Shears); ok && !m.Baby() {
		c.Shear(m)
		return held.Damage(1), true
	}
	if c.variant == MooshroomBrown && !m.Baby() {
		name := janimal.JavaItemName(held.Item())
		if e, ok := StewEffectOf(name); ok && !held.Empty() {
			if c.stew != nil {
				smokeParticles(m)
				return held, true
			}
			c.stew, c.stewItem = &e, name
			playSound(m, "mob.mooshroom.eat")
			if janimal.Creative(user) {
				return held, true
			}
			return held.Grow(-1), true
		}
	}
	if h, ok := MilkCow(m, user, held); ok {
		return h, true
	}
	return c.Base.MobInteract(m, user, held)
}

// Shear is MushroomCow.shear: the mooshroom becomes a cow (Mob.convertTo keeps name, health, age...) with an
// explosion particle and drops 5 mushrooms of its variant one block up.
func (c *Mooshroom) Shear(m *javamob.Mob) {
	playSound(m, "mob.mooshroom.shear")
	pos := m.Position()
	explosionParticle(m)
	if convertTo(m, "minecraft:cow") {
		if it, ok := javaItem(c.variant.MushroomItem()); ok {
			for range MooshroomShearDrops {
				dropAt(m.Tx(), pos.Add([3]float64{0, m.Height(), 0}), item.NewStack(it, 1))
			}
		}
	}
}

// ThunderHit is MushroomCow.thunderHit.
func (c *Mooshroom) ThunderHit(m *javamob.Mob, bolt uuid.UUID) {
	if v, changed := MooshroomLightning(c.variant, &c.lastLightning, bolt); changed {
		c.variant = v
		c.show(m)
		playSoundVol(m, "mob.mooshroom.convert", 2)
	}
}

// BreedOffspring is MushroomCow.getBreedOffspring.
func (c *Mooshroom) BreedOffspring(self, partner *janimal.Body) (string, func(*javamob.Mob), bool) {
	other := c.variant
	if p, ok := partner.B.(*Mooshroom); ok {
		other = p.variant
	}
	v := MooshroomOffspring(self.Rand(), c.variant, other)
	return "minecraft:mooshroom", func(baby *javamob.Mob) {
		if b, ok := baby.Behaviour().(*Mooshroom); ok {
			b.variant = v
			b.show(baby)
		}
	}, true
}

// FinalizeSpawn is AgeableMob.finalizeSpawn (MushroomCow keeps its default red).
func (c *Mooshroom) FinalizeSpawn(m *javamob.Mob, _ javamob.SpawnReason) {
	c.show(m)
}

// EncodeNBT is MushroomCow.addAdditionalSaveData: Type and stew_effects.
func (c *Mooshroom) EncodeNBT(data map[string]any) {
	c.Base.EncodeNBT(data)
	data["Type"] = c.variant.Name()
	if c.stew != nil {
		data["stew_effects"] = []any{map[string]any{"id": "minecraft:" + c.stewItem, "duration": c.stew.Ticks}}
	}
}

// DecodeNBT is MushroomCow.readAdditionalSaveData.
func (c *Mooshroom) DecodeNBT(m *javamob.Mob, data map[string]any) {
	c.Base.DecodeNBT(m, data)
	if t, _ := data["Type"].(string); t == "brown" {
		c.variant = MooshroomBrown
	}
	if l, ok := data["stew_effects"].([]any); ok && len(l) > 0 {
		if e, ok := l[0].(map[string]any); ok {
			name, _ := e["id"].(string)
			if se, ok := StewEffectOf(trimNS(name)); ok {
				c.stew, c.stewItem = &se, trimNS(name)
			}
		}
	}
	if m != nil {
		c.show(m)
	}
}
