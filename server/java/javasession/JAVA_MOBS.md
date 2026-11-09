# Mobs for Java players (javasession)

How javasession shows vanilla mobs to Java 26.2/26.3 clients. Code: `mob_iface.go` (interfaces),
`mobdata.go` (entity data, equipment, events), `mob_tables.go` (Bedrock -> Java tables),
`entity_types.go` (type names, spawn data), `entity.go` (head yaw), `self.go` (shared flags).
Tests: `mob_test.go` (bytes checked against the 26.3 layout).

## Interfaces the Mob type should implement

javasession never imports the mob package. It looks each method up on the entity, then on its
`Behaviour()` if it is an `*entity.Ent`. All are optional; a missing one means "default".
Names and value ranges are the Bedrock ones, so `server/session` can read the same methods.

| Method | Meaning (Bedrock source) |
|---|---|
| `Baby() bool` | `minecraft:is_baby` |
| `Variant() int32` / `MarkVariant() int32` | `minecraft:variant` / `minecraft:mark_variant` |
| `Colour() int32` / `Colour2() int32` | `minecraft:color` / `color2`, dye id 0 white .. 15 black |
| `Sheared()`, `Saddled()`, `Chested()`, `Tamed()`, `Sitting() bool` | the `is_*` components / sittable |
| `Charged() bool` (or `Powered()`) | charged creeper |
| `Ignited() bool` | creeper swelling (fuse lit) |
| `Climbing() bool` | spider on a wall |
| `Shaking() bool` | zombie -> drowned, skeleton -> stray, strider out of lava |
| `Converting() bool` | zombie villager being cured |
| `Angry() bool` | `minecraft:angry` (entity.Ent already has it) |
| `Aggressive() bool` | after a target: arms out, bow raised, weapon up |
| `UsingItem() bool` | drawing a bow, a witch drinking |
| `Sneaking() bool` | fox crouching |
| `Invisible() bool`, `OnFireDuration() time.Duration` | shared flags |
| `TradeTier() int32` | villager tier 0..4 |
| `ActorProperties() map[string]any` | description.properties; enum values as `string` (`minecraft:climate_variant`, `minecraft:sound_variant` are read) |
| `BodyYaw() float64`, `HeadYaw() float64` | separate body/head rotation in degrees (else both `Rotation().Yaw()`) |
| `Health() float64`, `MaxHealth() float64` | entity.Living |
| `HeldItems()` (item.Carrier), `Armour() *inventory.Armour` | hands and armour, as for players |
| `BodyArmour() item.Stack` | horse/wolf armour, llama carpet, harness: Java BODY slot |
| `Scale() float64` | salmon size |
| `Owner() *world.EntityHandle` | fishing hook caster (or `ProjectileOwner()`) |
| painting: `Facing() cube.Direction`, `Motive() string`, optional `HangingBlock() cube.Pos` | |

Actions: a `world.EntityAction` with `ActorEvent() uint8` returning a gophertunnel
`packet.ActorEvent*` id is played as a Java entity_event (the Bedrock session can send the same id
as an ActorEvent packet).

When state changes, call `ViewEntityState` on the viewers (as `Ent.RefreshState` does): it sends the
shared flags, mob data and saddle/body armour, each only when changed. Hurt actions refresh health.

## What is sent

Every Mob: J9 health FLOAT, J15 mob flags (0x04 aggressive = `Aggressive() || Angry()`).
Living entities and players: J0 flags (0x01 fire, 0x02 sneak, 0x08 sprint, 0x10 swim, 0x20
invisible, 0x80 glide), J6 pose, J8 hand state (0x01 using item). Fixed: the fire flag never showed
(the check wanted `OnFireDuration() int`, Dragonfly returns `time.Duration`), also for players.

| Java type | Data |
|---|---|
| every AgeableMob | J16 baby |
| zombie, husk, drowned, zombified_piglin, zombie_villager | J16 baby, J18 Shaking (drowned conversion) |
| cat, wolf, parrot, nautilus, zombie_nautilus | J18 flags 0x01 sitting, 0x04 tame |
| horses, camels, donkey, mule, llamas | J18 flags 0x02 tame; donkey/mule/llamas J19 chest |
| cow / chicken | J18 COW/CHICKEN_VARIANT from climate_variant; J19 sound variant |
| pig | J19 PIG_VARIANT, J20 PIG_SOUND (Bedrock `default` -> `classic`); saddle = SADDLE slot |
| sheep | J18 BYTE colour \| 0x10 sheared |
| frog | J18 FROG_VARIANT: Bedrock 0 temperate, 1 cold, 2 warm |
| cat | J20 CAT_VARIANT (white, black, red, siamese, british_shorthair, calico, persian, ragdoll, tabby, all_black, jellie), J23 collar, J24 sound |
| wolf | J21 collar, J22 anger end LONG (MaxInt64 angry, -1 calm), J23 WOLF_VARIANT (pale, ashen, black, chestnut, rusty, snowy, spotted, striped, woods), J24 sound (`mad` -> `angry`) |
| bee | J19 anger end LONG |
| parrot / rabbit / mooshroom / fox / llama | Variant unchanged: J20 / J18 / J18 / J18 / J21; fox J19 0x01 sitting 0x04 crouching |
| axolotl | J18 Variant with 1 and 3 swapped |
| horse | J19 Variant \| MarkVariant << 8 |
| panda | J21 and J22 genes = Variant |
| slime, magma_cube | J18 size = Variant (1, 2, 4) |
| villager (villager_v2) | J19 VILLAGER_DATA: type from MarkVariant (plains desert jungle savanna snow swamp taiga), profession from Variant (none farmer fisherman shepherd fletcher librarian cartographer cleric armorer weaponsmith toolsmith butcher leatherworker mason nitwit), level TradeTier + 1 |
| zombie_villager (v2) | J19 Converting, J20 VILLAGER_DATA |
| tropical_fish | J17 Variant \| MarkVariant << 8 \| Colour << 16 \| Colour2 << 24 |
| shulker | J18 colour = 15 - Variant (16 = default) |
| salmon | J17 type 0/1/2 from Scale 0.5/1/1.5 |
| skeleton | J16 Shaking (stray conversion) |
| bogged | J16 Sheared |
| spider, cave_spider | J16 0x01 Climbing |
| creeper | J16 swell (1 Ignited, -1 idle), J17 Charged/Powered |
| snow_golem | J16 0x10 pumpkin unless Sheared |
| strider | J19 Shaking |
| enderman | J17 screaming = Angry |
| piglin / zoglin | J17 / J16 baby |
| ghast | J16 charging = Charged |
| witch | J17 drinking = UsingItem |
| wither_skull | J8 dangerous for `wither_skull_dangerous` |
| painting | J9 PAINTING_VARIANT = registry id + 1 from the snake-cased motive |

Holder serializers (variants) carry the network id of the entry in the registry_data the client got
(v777.Registries; remapped with `ver.Synced` for 26.2). Villager type/profession are built-in
registry protocol ids (the same in 26.2 and 26.3). The 26.2 entity data layout is identical
(compared defineId order in both decompiles).

Equipment: Java EquipmentSlot ordinals, BODY 6 and SADDLE 7, in set_equipment.

Entity events: StartAttacking 4, TamingFailed 6, TamingSucceeded 7, ShakeWetness 8, EatGrass 10,
StartOfferFlower 11, StopOfferFlower 34, (In)LoveHearts 18 (villager 12), VillagerAngry 13,
VillagerHappy 14, WitchHatMagic 15, ZombieConverting 16, GuardianAttackSound 21.

## Type names and spawn data

- Renamed: tropicalfish -> tropical_fish, eye_of_ender_signal -> eye_of_ender,
  breeze_wind_charge_projectile -> breeze_wind_charge, wither_skull_dangerous -> wither_skull.
  Hidden: elder_guardian_ghost, balloon, ice_bomb (plus agent, npc, tripod_camera as before).
- boat / chest_boat: Java type per wood from Variant (oak spruce birch jungle acacia dark_oak
  mangrove bamboo cherry pale_oak; oak if unknown); Java yaw = Bedrock yaw - 90.
- painting: add_entity data = 3D direction of Facing (north 2, south 3, west 4, east 5; never 0,
  which disconnects the client), position = the hanging block (HangingBlock, else the block holding
  Position()).
- fishing_bobber: data = Java id of the casting player; not spawned if the client does not know the
  owner (the client would discard it).
- add_entity carries body and head yaw; move packets the body, rotate_head the head.
- The state (flags, mob data, saddle) is sent right after add_entity.
- A test resolves every entity of the 1.26.63 vanilla pack to a 26.3 type.
- mobdata_states.go: sniffer state from its goal flags, armadillo state (with the roll-up and peek)
  and creaking flags from their enum properties, happy ghast STAYS_STILL from the collidable flag.

## Not done

Bedrock-only state that has no interface yet: shulker attach face/peek, enderman carried block,
guardian/wither targets, goat horns, copper golem states, camel sitting,
puffer state, phantom size, villager/fox sleeping, scale attribute. Arrow/projectile owner data and
tamed owner UUIDs are not sent.
