package javasession

import (
	"bytes"
	"math"
	"strconv"

	"github.com/df-mc/dragonfly/server/item"
	jitem "github.com/df-mc/dragonfly/server/java/protocol/item"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/java/protocol/wire"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// Java entity data of mobs. 26.2 and 26.3 have the same layout (SynchedEntityData.defineId order
// in both decompiles); JAVA_MOBS.md lists every index used.
const (
	// LivingEntity.DATA_LIVING_ENTITY_FLAGS: 0x01 using an item, 0x02 with the off hand.
	dataLivingFlags = 8
	// Mob.DATA_MOB_FLAGS_ID: 0x01 no AI, 0x02 left handed, 0x04 aggressive. A mob after a target
	// is aggressive: an illager raises its weapon, a zombie holds its arms out, a skeleton its bow.
	dataMobFlags      = 15
	mobFlagAggressive = 0x04

	dataTypeLong            = 2
	dataTypeVillagerData    = 18
	dataTypeCatVariant      = 21
	dataTypeCatSound        = 22
	dataTypeCowVariant      = 23
	dataTypeCowSound        = 24
	dataTypeWolfVariant     = 25
	dataTypeWolfSound       = 26
	dataTypeFrogVariant     = 27
	dataTypePigVariant      = 28
	dataTypePigSound        = 29
	dataTypeChickenVariant  = 30
	dataTypeChickenSound    = 31
	dataTypePaintingVariant = 34
)

// Java equipment slots only animals have (EquipmentSlot ordinals, after equipHead).
const (
	equipBody   = 6
	equipSaddle = 7
)

// mobData collects set_entity_data entries.
type mobData struct {
	w *wire.Writer
	n int
}

func (d *mobData) entry(index byte, serializer int32) {
	d.w.Byte(index)
	d.w.VarInt(serializer)
	d.n++
}

func (d *mobData) byteVal(index, v byte) { d.entry(index, dataTypeByte); d.w.Byte(v) }
func (d *mobData) boolean(index byte, v bool) {
	d.entry(index, dataTypeBoolean)
	d.w.Bool(v)
}
func (d *mobData) varInt(index byte, v int32)  { d.entry(index, dataTypeInt); d.w.VarInt(v) }
func (d *mobData) long(index byte, v int64)    { d.entry(index, dataTypeLong); d.w.VarLong(v) }
func (d *mobData) float(index byte, v float32) { d.entry(index, dataTypeFloat); d.w.Float32(v) }
func (d *mobData) holder(index byte, serializer, id int32) {
	if id < 0 {
		return // unknown entry: the client keeps its default
	}
	d.entry(index, serializer)
	d.w.VarInt(id)
}

// mobKind is the entity data a Java entity type has past the LivingEntity data.
type mobKind struct {
	notMob  bool // not a Mob: no mob flags or health (projectiles, paintings)
	ageable bool // AgeableMob: 16 baby
	zombie  bool // Zombie: 16 baby, 18 drowned conversion
	tamable bool // TamableAnimal: 18 flags (0x01 sitting, 0x04 tame)
	horse   bool // AbstractHorse: 18 flags (0x02 tame)
	chested bool // AbstractChestedHorse: 19 has chest
	// extra writes the type's own data. bedrock is the Bedrock entity id.
	extra func(s *Session, d *mobData, e world.Entity, bedrock string)
}

// mobKinds are the Java entity types javasession sends mob data for, by Java name.
var mobKinds = map[string]*mobKind{}

func init() {
	plain := []string{
		"allay", "bat", "breeze", "cod", "copper_golem", "creaking", "elder_guardian",
		"endermite", "ender_dragon", "evoker", "giant", "guardian", "illusioner", "iron_golem",
		"parched", "phantom", "piglin_brute", "pillager", "pufferfish", "ravager", "silverfish",
		"stray", "tadpole", "vex", "vindicator", "warden", "wither", "wither_skeleton",
	}
	for _, n := range plain {
		mobKinds["minecraft:"+n] = &mobKind{}
	}
	ageable := []string{
		"armadillo", "dolphin", "glow_squid", "goat", "happy_ghast", "hoglin", "ocelot",
		"polar_bear", "sniffer", "squid", "sulfur_cube", "turtle", "wandering_trader",
	}
	for _, n := range ageable {
		mobKinds["minecraft:"+n] = &mobKind{ageable: true}
	}
	for _, n := range []string{"zombie", "husk", "drowned", "zombified_piglin"} {
		mobKinds["minecraft:"+n] = &mobKind{zombie: true}
	}
	for _, n := range []string{"camel", "camel_husk", "horse", "skeleton_horse", "zombie_horse"} {
		mobKinds["minecraft:"+n] = &mobKind{ageable: true, horse: true}
	}
	for _, n := range []string{"donkey", "mule", "llama", "trader_llama"} {
		mobKinds["minecraft:"+n] = &mobKind{ageable: true, horse: true, chested: true}
	}
	for _, n := range []string{"nautilus", "zombie_nautilus", "parrot", "cat", "wolf"} {
		mobKinds["minecraft:"+n] = &mobKind{ageable: true, tamable: true}
	}
	for _, n := range []string{"slime", "magma_cube"} {
		mobKinds["minecraft:"+n] = &mobKind{ageable: true, extra: cubeData}
	}
	for _, n := range []string{"spider", "cave_spider"} {
		mobKinds["minecraft:"+n] = &mobKind{extra: spiderData}
	}
	set := func(name string, k mobKind) { mobKinds["minecraft:"+name] = &k }
	set("cow", mobKind{ageable: true, extra: func(s *Session, d *mobData, e world.Entity, _ string) {
		s.climateVariant(d, e, 18, dataTypeCowVariant, "minecraft:cow_variant")
		s.soundVariant(d, e, 19, dataTypeCowSound, "minecraft:cow")
	}})
	set("pig", mobKind{ageable: true, extra: func(s *Session, d *mobData, e world.Entity, _ string) {
		s.climateVariant(d, e, 19, dataTypePigVariant, "minecraft:pig_variant")
		s.soundVariant(d, e, 20, dataTypePigSound, "minecraft:pig")
	}})
	set("chicken", mobKind{ageable: true, extra: func(s *Session, d *mobData, e world.Entity, _ string) {
		s.climateVariant(d, e, 18, dataTypeChickenVariant, "minecraft:chicken_variant")
		s.soundVariant(d, e, 19, dataTypeChickenSound, "minecraft:chicken")
	}})
	set("sheep", mobKind{ageable: true, extra: func(_ *Session, d *mobData, e world.Entity, _ string) {
		// Sheep.DATA_WOOL_ID: the dye id in the low 4 bits (the same ids on both editions), 0x10 sheared.
		wool := byte(mobInt(e, mobColour.Colour, 0)) & 0x0f
		d.byteVal(18, wool|bit(mobBool(e, mobSheared.Sheared), 0x10))
	}})
	set("frog", mobKind{ageable: true, extra: func(s *Session, d *mobData, e world.Entity, _ string) {
		if v, ok := mobAs[mobVariant](e); ok {
			d.holder(18, dataTypeFrogVariant, s.syncedEntry("minecraft:frog_variant", nth(bedrockFrogVariant, v.Variant())))
		}
	}})
	mobKinds["minecraft:cat"].extra = func(s *Session, d *mobData, e world.Entity, _ string) {
		if v, ok := mobAs[mobVariant](e); ok {
			d.holder(20, dataTypeCatVariant, s.syncedEntry("minecraft:cat_variant", nth(bedrockCatVariant, v.Variant())))
		}
		if c, ok := mobAs[mobColour](e); ok {
			d.varInt(23, c.Colour()) // collar, drawn only when tame
		}
		s.soundVariant(d, e, 24, dataTypeCatSound, "minecraft:cat")
	}
	mobKinds["minecraft:wolf"].extra = func(s *Session, d *mobData, e world.Entity, _ string) {
		if c, ok := mobAs[mobColour](e); ok {
			d.varInt(21, c.Colour()) // collar
		}
		// Wolf.DATA_ANGER_END_TIME: angry while it is past the client's game time.
		d.long(22, angerEndTime(e))
		if v, ok := mobAs[mobVariant](e); ok {
			d.holder(23, dataTypeWolfVariant, s.syncedEntry("minecraft:wolf_variant", nth(bedrockWolfVariant, v.Variant())))
		}
		s.soundVariant(d, e, 24, dataTypeWolfSound, "minecraft:wolf")
	}
	mobKinds["minecraft:parrot"].extra = intVariant(20) // the same ids: red_blue, blue, green, yellow_blue, gray
	set("rabbit", mobKind{ageable: true, extra: intVariant(18)})
	set("mooshroom", mobKind{ageable: true, extra: intVariant(18)})
	mobKinds["minecraft:llama"].extra = intVariant(21)
	mobKinds["minecraft:trader_llama"].extra = intVariant(21)
	set("axolotl", mobKind{ageable: true, extra: func(_ *Session, d *mobData, e world.Entity, _ string) {
		if v, ok := mobAs[mobVariant](e); ok {
			// Bedrock 1 cyan / 3 wild, Java 1 wild / 3 cyan; lucy 0, gold 2 and blue 4 are the same.
			n := v.Variant()
			switch n {
			case 1:
				n = 3
			case 3:
				n = 1
			}
			d.varInt(18, n)
		}
	}})
	mobKinds["minecraft:horse"].extra = func(_ *Session, d *mobData, e world.Entity, _ string) {
		_, hasV := mobAs[mobVariant](e)
		_, hasM := mobAs[mobMarkVariant](e)
		if hasV || hasM {
			// Horse.DATA_ID_TYPE_VARIANT: colour | markings << 8, the same numbers on both editions.
			d.varInt(19, mobInt(e, mobVariant.Variant, 0)&0xff|(mobInt(e, mobMarkVariant.MarkVariant, 0)&0xff)<<8)
		}
	}
	set("fox", mobKind{ageable: true, extra: func(_ *Session, d *mobData, e world.Entity, _ string) {
		if v, ok := mobAs[mobVariant](e); ok {
			d.varInt(18, v.Variant()) // 0 red, 1 snow
		}
		sneaking := mobBool(e, mobSneaking.Sneaking)
		d.byteVal(19, bit(mobBool(e, mobSitting.Sitting), 0x01)|bit(sneaking, 0x04))
	}})
	set("panda", mobKind{ageable: true, extra: func(_ *Session, d *mobData, e world.Entity, _ string) {
		if v, ok := mobAs[mobVariant](e); ok {
			// Bedrock sends only the gene shown: both Java genes get it, so recessive ones show too.
			g := byte(v.Variant())
			d.byteVal(21, g)
			d.byteVal(22, g)
		}
	}})
	set("villager", mobKind{ageable: true, extra: func(s *Session, d *mobData, e world.Entity, bedrock string) {
		if bedrock == "minecraft:villager_v2" {
			villagerData(d, e, 19)
		}
	}})
	set("zombie_villager", mobKind{zombie: true, extra: func(s *Session, d *mobData, e world.Entity, bedrock string) {
		d.boolean(19, mobBool(e, mobConverting.Converting))
		if bedrock == "minecraft:zombie_villager_v2" {
			villagerData(d, e, 20)
		}
	}})
	set("tropical_fish", mobKind{extra: func(_ *Session, d *mobData, e world.Entity, _ string) {
		if _, ok := mobAs[mobVariant](e); !ok {
			return
		}
		// TropicalFish.packVariant: shape | pattern << 8 | base colour << 16 | pattern colour << 24.
		shape := mobInt(e, mobVariant.Variant, 0) & 0xff
		pattern := mobInt(e, mobMarkVariant.MarkVariant, 0) & 0xff
		base := mobInt(e, mobColour.Colour, 0) & 0xff
		patternColour := mobInt(e, mobColour2.Colour2, 0) & 0xff
		d.varInt(17, shape|pattern<<8|base<<16|patternColour<<24)
	}})
	set("shulker", mobKind{extra: func(_ *Session, d *mobData, e world.Entity, _ string) {
		if v, ok := mobAs[mobVariant](e); ok {
			// Bedrock 16 is the default purple shulker (Java 16 too); the dyed ones run backwards.
			c := byte(16)
			if n := v.Variant(); n >= 0 && n < 16 {
				c = byte(15 - n)
			}
			d.byteVal(18, c)
		}
	}})
	set("salmon", mobKind{extra: func(_ *Session, d *mobData, e world.Entity, _ string) {
		if sc, ok := mobAs[mobScale](e); ok {
			// Bedrock salmon sizes are scales 0.5, 1 and 1.5; Java's are types small, medium, large.
			t := int32(1)
			switch v := sc.Scale(); {
			case v < 0.75:
				t = 0
			case v > 1.25:
				t = 2
			}
			d.varInt(17, t)
		}
	}})
	set("skeleton", mobKind{extra: func(_ *Session, d *mobData, e world.Entity, _ string) {
		d.boolean(16, mobBool(e, mobShaking.Shaking)) // turning into a stray
	}})
	set("bogged", mobKind{extra: func(_ *Session, d *mobData, e world.Entity, _ string) {
		d.boolean(16, mobBool(e, mobSheared.Sheared))
	}})
	set("creeper", mobKind{extra: func(_ *Session, d *mobData, e world.Entity, _ string) {
		swell := int32(-1)
		if mobBool(e, mobIgnited.Ignited) {
			swell = 1
		}
		d.varInt(16, swell)
		d.boolean(17, mobBool(e, mobCharged.Charged) || mobBool(e, mobPowered.Powered))
	}})
	set("snow_golem", mobKind{extra: func(_ *Session, d *mobData, e world.Entity, _ string) {
		d.byteVal(16, bit(!mobBool(e, mobSheared.Sheared), 0x10)) // 0x10 wears its pumpkin
	}})
	set("strider", mobKind{ageable: true, extra: func(_ *Session, d *mobData, e world.Entity, _ string) {
		d.boolean(19, mobBool(e, mobShaking.Shaking)) // cold, out of lava
	}})
	set("enderman", mobKind{extra: func(_ *Session, d *mobData, e world.Entity, _ string) {
		d.boolean(17, mobBool(e, mobAngry.Angry)) // screaming, jaw open
	}})
	set("bee", mobKind{ageable: true, extra: func(_ *Session, d *mobData, e world.Entity, _ string) {
		d.long(19, angerEndTime(e))
	}})
	set("piglin", mobKind{extra: func(_ *Session, d *mobData, e world.Entity, _ string) {
		d.boolean(17, mobBool(e, mobBaby.Baby))
	}})
	set("zoglin", mobKind{extra: func(_ *Session, d *mobData, e world.Entity, _ string) {
		d.boolean(16, mobBool(e, mobBaby.Baby))
	}})
	set("ghast", mobKind{extra: ghastData})
	set("blaze", mobKind{extra: blazeData})
	set("witch", mobKind{extra: func(_ *Session, d *mobData, e world.Entity, _ string) {
		d.boolean(17, mobBool(e, mobUsing.UsingItem)) // drinking a potion
	}})

	set("wither_skull", mobKind{notMob: true, extra: func(_ *Session, d *mobData, _ world.Entity, bedrock string) {
		d.boolean(8, bedrock == "minecraft:wither_skull_dangerous") // the blue skull
	}})
	set("painting", mobKind{notMob: true, extra: func(s *Session, d *mobData, e world.Entity, _ string) {
		if m, ok := mobAs[paintingMotive](e); ok {
			// Painting.DATA_PAINTING_VARIANT_ID is a holder: registry id + 1 (0 an inline variant).
			if id := s.syncedEntry("minecraft:painting_variant", paintingVariantName(m.Motive())); id >= 0 {
				d.holder(9, dataTypePaintingVariant, id+1)
			}
		}
	}})
}

// bit is b if v is true, else 0.
func bit(v bool, b byte) byte {
	if v {
		return b
	}
	return 0
}

// intVariant writes the Bedrock Variant unchanged as the INT at index.
func intVariant(index byte) func(*Session, *mobData, world.Entity, string) {
	return func(_ *Session, d *mobData, e world.Entity, _ string) {
		if v, ok := mobAs[mobVariant](e); ok {
			d.varInt(index, v.Variant())
		}
	}
}

// cubeData is a slime's or magma cube's size (AbstractCubeMob.ID_SIZE): the Bedrock Variant is the
// size, 1, 2 or 4.
func cubeData(_ *Session, d *mobData, e world.Entity, _ string) {
	if v, ok := mobAs[mobVariant](e); ok {
		d.varInt(18, max(1, v.Variant()))
	}
}

// spiderData is a spider's flags (Spider.DATA_FLAGS_ID): 0x01 climbing a wall.
func spiderData(_ *Session, d *mobData, e world.Entity, _ string) {
	d.byteVal(16, bit(mobBool(e, mobClimbing.Climbing), 0x01))
}

// villagerData writes VILLAGER_DATA at index: type, profession and level, from the Bedrock
// villager_v2 MarkVariant, Variant and TradeTier.
func villagerData(d *mobData, e world.Entity, index byte) {
	typ, ok := javaVillagerType[nth(bedrockVillagerType, mobInt(e, mobMarkVariant.MarkVariant, 0))]
	if !ok {
		typ = javaVillagerType["plains"]
	}
	prof := javaVillagerProfession[nth(bedrockVillagerProfession, mobInt(e, mobVariant.Variant, 0))] // none if unknown
	d.entry(index, dataTypeVillagerData)
	d.w.VarInt(typ)
	d.w.VarInt(prof)
	d.w.VarInt(max(1, mobInt(e, mobTradeTier.TradeTier, 0)+1))
}

// angerEndTime is a neutral mob's DATA_ANGER_END_TIME: the client takes it as angry while the end
// time is past its game time, so an angry mob gets the largest one and a calm one none (-1).
func angerEndTime(e world.Entity) int64 {
	if mobBool(e, mobAngry.Angry) {
		return math.MaxInt64
	}
	return -1
}

// syncedEntry is this client's network id of minecraft:<entry> in a synchronised registry, or -1.
func (s *Session) syncedEntry(registry, entry string) int32 {
	id := registryEntry(registry, entry)
	if id < 0 {
		return -1
	}
	return s.ver.Synced(registry, id)
}

// climateVariant writes a cow, pig or chicken variant from the Bedrock actor property
// minecraft:climate_variant (temperate, warm, cold: the Java variant names).
func (s *Session) climateVariant(d *mobData, e world.Entity, index byte, serializer int32, registry string) {
	if v := mobProperty(e, "minecraft:climate_variant"); v != "" {
		d.holder(index, serializer, s.syncedEntry(registry, v))
	}
}

// soundVariant writes a mob's sound variant from the Bedrock actor property minecraft:sound_variant.
func (s *Session) soundVariant(d *mobData, e world.Entity, index byte, serializer int32, mob string) {
	v := mobProperty(e, "minecraft:sound_variant")
	if v == "" {
		return
	}
	if j, ok := bedrockSoundVariant[mob][v]; ok {
		v = j
	}
	d.holder(index, serializer, s.syncedEntry(mob+"_sound_variant", v))
}

// writeMobData writes the entity data of e as a Java entity of kind k.
func (s *Session) writeMobData(d *mobData, k *mobKind, e world.Entity, bedrock string) {
	if !k.notMob {
		if h, ok := mobAs[mobHealth](e); ok {
			d.float(dataLivingHealth, float32(h.Health()))
		}
		aggressive := mobBool(e, mobAggressive.Aggressive) || mobBool(e, mobAngry.Angry)
		d.byteVal(dataMobFlags, bit(aggressive, mobFlagAggressive))
	}
	if k.ageable || k.zombie {
		d.boolean(16, mobBool(e, mobBaby.Baby))
	}
	if k.zombie {
		d.boolean(18, mobBool(e, mobShaking.Shaking)) // turning into a drowned
	}
	if k.tamable {
		d.byteVal(18, bit(mobBool(e, mobSitting.Sitting), 0x01)|bit(mobBool(e, mobTamed.Tamed), 0x04))
	}
	if k.horse {
		d.byteVal(18, bit(mobBool(e, mobTamed.Tamed), 0x02))
	}
	if k.chested {
		d.boolean(19, mobBool(e, mobChested.Chested))
	}
	if k.extra != nil {
		k.extra(s, d, e, bedrock)
	}
}

// viewMobData sends the mob data of e (mob flags, health, baby, variants, colours...) when it
// changed since it was last sent for id. Entities whose Java type has no mob data get nothing.
func (s *Session) viewMobData(e world.Entity, id int32) {
	name, ok := javaTypeName(e)
	if !ok {
		return
	}
	k := mobKinds[name]
	if k == nil {
		return
	}
	w := s.packet()
	w.VarInt(id)
	start := len(w.B)
	d := mobData{w: w}
	s.writeMobData(&d, k, e, bedrockName(e))
	body := w.B[start:]
	s.entMu.Lock()
	if s.mobData == nil {
		s.mobData = map[int32][]byte{}
	}
	if d.n == 0 || bytes.Equal(s.mobData[id], body) {
		s.entMu.Unlock()
		s.writers.Put(w)
		return
	}
	s.mobData[id] = bytes.Clone(body)
	s.entMu.Unlock()
	w.Byte(0xff)
	s.queue(v777.ClientboundPlaySetEntityData, w)
}

// saddleItem is the Java id of minecraft:saddle.
var saddleItem, _ = jitem.ByName("minecraft:saddle")

// viewMobEquipment sends the saddle and body armour of a mob (the Java SADDLE and BODY slots) when
// they changed since they were last sent for id.
func (s *Session) viewMobEquipment(e world.Entity, id int32) {
	sd, hasSaddle := mobAs[mobSaddled](e)
	ba, hasBody := mobAs[mobBodyArmour](e)
	if !hasSaddle && !hasBody {
		return
	}
	saddled := hasSaddle && sd.Saddled()
	var body item.Stack
	if hasBody {
		body = ba.BodyArmour()
	}
	key := strconv.FormatBool(saddled)
	if !body.Empty() {
		name, meta := body.Item().EncodeItem()
		key += "|" + name + ":" + strconv.Itoa(int(meta))
	}
	s.entMu.Lock()
	if s.mobEquip == nil {
		s.mobEquip = map[int32]string{}
	}
	if last, sent := s.mobEquip[id]; sent && last == key || !sent && key == "false" {
		s.entMu.Unlock()
		return
	}
	s.mobEquip[id] = key
	s.entMu.Unlock()

	w := s.packet()
	w.VarInt(id)
	w.Byte(equipBody | 0x80)
	s.writeStack(w, body)
	w.Byte(equipSaddle)
	if saddled && saddleItem >= 0 {
		js := jitem.Stack{Count: 1, ID: saddleItem}
		js.EncodeFor(w, s.items().proto)
	} else {
		jitem.WriteEmpty(w)
	}
	s.queue(v777.ClientboundPlaySetEquipment, w)
}

// javaEntityEvents maps Bedrock actor events (packet.ActorEvent*) to Java entity events
// (EntityEvent ids) for mob actions that are actor events (mobActorEvent).
var javaEntityEvents = map[uint8]byte{
	packet.ActorEventStartAttacking:      4, // iron golem, ravager, hoglin, zoglin, evoker fangs attack
	packet.ActorEventTamingFailed:        6, // smoke
	packet.ActorEventTamingSucceeded:     7, // hearts
	packet.ActorEventShakeWetness:        8, // a wolf shaking itself dry
	packet.ActorEventEatGrass:            10,
	packet.ActorEventStartOfferFlower:    11,
	packet.ActorEventStopOfferFlower:     34,
	packet.ActorEventLoveHearts:          18,
	packet.ActorEventInLoveHearts:        18,
	packet.ActorEventVillagerAngry:       13,
	packet.ActorEventVillagerHappy:       14,
	packet.ActorEventWitchHatMagic:       15,
	packet.ActorEventZombieConverting:    16,
	packet.ActorEventGuardianAttackSound: 21,
}

// viewActorEvent plays a Bedrock actor event of the entity e (Java id id) as its Java entity event.
func (s *Session) viewActorEvent(e world.Entity, id int32, ev uint8) {
	j, ok := javaEntityEvents[ev]
	if !ok {
		return
	}
	if j == 18 {
		if name, _ := javaTypeName(e); name == "minecraft:villager" {
			j = 12 // EntityEvent.LOVE_HEARTS: a villager's are its own event
		}
	}
	w := s.packet()
	w.Int32(id)
	w.Byte(j)
	s.queue(v777.ClientboundPlayEntityEvent, w)
}
