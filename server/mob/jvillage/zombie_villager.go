//go:build crossfly_full

package jvillage

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/monster"
	"github.com/df-mc/dragonfly/server/mob/jbrain"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// ZombieVillagerID is the zombie villager's identifier (the Bedrock
// zombie_villager_v2 both clients render).
const ZombieVillagerID = "minecraft:zombie_villager_v2"

func init() {
	javamob.Register(javamob.Type{
		ID: ZombieVillagerID, Category: javamob.CategoryMonster,
		Width: 0.6, Height: 1.95, EyeHeight: 1.74,
		Attributes:      monster.ZombieAttributes,
		BurnsInDaylight: true, Undead: true,
		Families: []string{"zombie_villager", "zombie", "undead", "monster", "mob"},
		XPReward: 5,
		New: func() javamob.Behaviour {
			z := monster.NewZombie()
			z.ConvertsTo = "" // ZombieVillager.convertsInWater: false
			return &ZombieVillager{Zombie: z, conversionTime: -1, Data: DefaultData()}
		},
	})
}

// ZombieVillager is Java's ZombieVillager: a zombie that remembers its
// villager life and can be cured.
type ZombieVillager struct {
	*monster.Zombie

	Data          Data
	DataFinalized bool

	conversionTime    int
	conversionStarter uuid.UUID
	gossips           *Gossips
	offers            Offers
	hasOffers         bool
	xp                int
}

// Converting is ZombieVillager.isConverting.
func (z *ZombieVillager) Converting() bool { return z.conversionTime >= 0 }

// Tick is ZombieVillager.tick: the cure counts down (faster near beds and
// iron bars), then the villager comes back.
func (z *ZombieVillager) Tick(m *javamob.Mob) {
	if m.Dead() || !z.Converting() {
		return
	}
	z.conversionTime -= z.conversionProgress(m)
	if z.conversionTime <= 0 {
		z.finishConversion(m)
	}
}

// conversionProgress is ZombieVillager.getConversionProgress.
func (z *ZombieVillager) conversionProgress(m *javamob.Mob) int {
	amount := 1
	r := m.Rand()
	if r.Float32() >= 0.01 {
		return amount
	}
	pos := m.Position()
	px, py, pz := int(pos[0]), int(pos[1]), int(pos[2])
	count := 0
	tx := m.Tx()
	for x := px - 4; x < px+4 && count < 14; x++ {
		for y := py - 4; y < py+4 && count < 14; y++ {
			for z2 := pz - 4; z2 < pz+4 && count < 14; z2++ {
				if speedsUpCuring(blockName(tx.Block(cube.Pos{x, y, z2}))) {
					if r.Float32() < 0.3 {
						amount++
					}
					count++
				}
			}
		}
	}
	return amount
}

// speedsUpCuring is #minecraft:speeds_up_zombie_villager_curing: iron bars
// and beds.
func speedsUpCuring(name string) bool {
	return name == "iron_bars" || name == "bed"
}

// MobInteract is ZombieVillager.mobInteract: a golden apple cures a
// weakened zombie villager.
func (z *ZombieVillager) MobInteract(m *javamob.Mob, user item.User, held item.Stack) (item.Stack, bool) {
	if ga, ok := held.Item().(item.GoldenApple); ok && !isEnchantedApple(ga) {
		if _, weak := m.Effect(effect.Weakness); !weak {
			return held, true
		}
		var id uuid.UUID
		if p, ok := user.(*player.Player); ok {
			id = p.UUID()
			if !p.GameMode().CreativeInventory() {
				held = held.Grow(-1)
			}
		}
		z.StartConverting(m, id, m.Rand().IntN(2401)+3600)
		return held, true
	}
	return held, false
}

func isEnchantedApple(item.GoldenApple) bool { return false }

// StartConverting is ZombieVillager.startConverting.
func (z *ZombieVillager) StartConverting(m *javamob.Mob, player uuid.UUID, ticks int) {
	z.conversionStarter = player
	z.conversionTime = ticks
	m.UpdateMeta(func(meta *mobMeta) { meta.Converting = true })
	m.RemoveEffect(effect.Weakness)
	lvl := min(m.DifficultyID()-1, 0)
	m.AddEffect(effect.New(effect.Strength, lvl+1, ticksDuration(ticks)))
	m.PlaySound("remedy")
}

// RemoveWhenFarAway is ZombieVillager.removeWhenFarAway.
func (z *ZombieVillager) RemoveWhenFarAway(m *javamob.Mob, d float64) bool {
	return !z.Converting() && z.xp == 0
}

// finishConversion is ZombieVillager.finishConversion: the cured villager
// keeps the data, offers, gossips and experience, and the curer gets the
// big discount (ZOMBIE_VILLAGER_CURED).
func (z *ZombieVillager) finishConversion(m *javamob.Mob) {
	tx := m.Tx()
	starter := z.conversionStarter
	baby := m.Baby()
	_, _ = m.ConvertTo(VillagerID, false, func(v *javamob.Mob) {
		vv, ok := Of(v)
		if !ok {
			return
		}
		vv.DataFinalized = z.DataFinalized
		vv.Data = z.Data
		if z.gossips != nil {
			vv.gossips.PutAll(z.gossips)
		}
		if z.hasOffers {
			vv.SetOffers(copyOffers(z.offers))
		}
		vv.xp = z.xp
		if baby {
			vv.SetAge(-24000)
		}
		vv.FinalizeSpawn(v, javamob.SpawnConversion)
		vv.refreshBrain(v)
		if starter != uuid.Nil {
			if p, ok := playerByUUID(tx, starter); ok {
				vv.OnReputationEvent(v, ReputationZombieVillagerCured, p)
			}
		}
		v.AddEffect(effect.New(effect.Nausea, 1, ticksDuration(200)))
	})
}

func playerByUUID(tx *world.Tx, id uuid.UUID) (*player.Player, bool) {
	for e := range tx.Players() {
		if p, ok := e.(*player.Player); ok && p.UUID() == id {
			return p, true
		}
	}
	return nil, false
}

func copyOffers(o Offers) Offers {
	out := make(Offers, len(o))
	for i, x := range o {
		out[i] = x.Copy()
	}
	return out
}

// convertVillager is Zombie.convertVillagerToZombieVillager.
func convertVillager(m *javamob.Mob) bool {
	v, ok := Of(m)
	if !ok {
		return false
	}
	v.ReleaseAllPois(m)
	_, ok = m.ConvertTo(ZombieVillagerID, true, func(zm *javamob.Mob) {
		z, ok := zm.Behaviour().(*ZombieVillager)
		if !ok {
			return
		}
		z.Data, z.DataFinalized = v.Data, v.DataFinalized
		z.gossips = v.gossips.Copy()
		if v.hasOffers {
			z.offers, z.hasOffers = copyOffers(v.offers), true
		}
		z.xp = v.xp
		z.syncData(zm)
		// Zombie.convertVillagerToZombieVillager: finalizeSpawn(CONVERSION).
		z.Zombie.FinalizeSpawn(zm, javamob.SpawnConversion)
	})
	return ok
}

// villagerKilledBy is Zombie.killedEntity for a villager: on normal (50%)
// and hard (always) difficulty the villager rises as a zombie villager.
func villagerKilledBy(m *javamob.Mob, killer world.Entity) bool {
	km, ok := javamob.Of(killer)
	if !ok {
		return false
	}
	if _, ok := km.Behaviour().(interface{ Converting() bool }); !ok {
		return false
	}
	if !isZombieKind(km.JavaType().ID) {
		return false
	}
	d := m.DifficultyID()
	if d < 2 {
		return false
	}
	if d != 3 && km.Rand().IntN(2) == 0 {
		return false
	}
	return convertVillager(m)
}

func isZombieKind(id string) bool {
	switch id {
	case "minecraft:zombie", "minecraft:husk", "minecraft:drowned", ZombieVillagerID, "minecraft:zombified_piglin":
		return true
	}
	return false
}

func (z *ZombieVillager) syncData(m *javamob.Mob) {
	v := &Villager{Data: z.Data, xp: z.xp}
	v.syncData(m)
}

// EncodeNBT writes the zombie and zombie villager data.
func (z *ZombieVillager) EncodeNBT(data map[string]any) {
	z.Zombie.EncodeNBT(data)
	data["VillagerData"] = z.Data.NBT()
	data["VillagerDataFinalized"] = boolByte(z.DataFinalized)
	if z.hasOffers {
		data["Offers"] = z.offers.NBT()
	}
	if z.gossips != nil {
		data["Gossips"] = z.gossips.NBT()
	}
	data["ConversionTime"] = int32(z.conversionTime)
	if z.conversionStarter != uuid.Nil {
		data["ConversionPlayer"] = jbrain.UUIDCodec.Encode(z.conversionStarter)
	}
	data["Xp"] = int32(z.xp)
}

// DecodeNBT reads EncodeNBT's data.
func (z *ZombieVillager) DecodeNBT(m *javamob.Mob, data map[string]any) {
	z.Zombie.DecodeNBT(m, data)
	if d, ok := data["VillagerData"].(map[string]any); ok {
		z.Data, z.DataFinalized = DataFromNBT(d), true
	}
	if o, ok := data["Offers"].(map[string]any); ok {
		z.offers, z.hasOffers = OffersFromNBT(o), true
	}
	if l, ok := data["Gossips"].([]any); ok {
		z.gossips = GossipsFromNBT(l)
	}
	z.conversionTime = int(i32(data["ConversionTime"], -1))
	z.xp = int(i32(data["Xp"], 0))
	z.syncData(m)
}

// FinalizeSpawn is ZombieVillager.finalizeSpawn: the biome type, then the
// zombie's own setup; a random profession like initializeZombieVillagerData.
func (z *ZombieVillager) FinalizeSpawn(m *javamob.Mob, reason javamob.SpawnReason) {
	if !z.DataFinalized {
		z.Data.Profession = Professions[m.Rand().IntN(len(Professions))]
		z.Data.Type = TypeByBedrockBiome(biomeName(m.Tx(), BlockPos(m)))
		z.DataFinalized = true
	}
	z.Zombie.FinalizeSpawn(m, reason)
	z.syncData(m)
}
