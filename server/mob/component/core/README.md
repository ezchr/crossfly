# server/mob/component/core

Passive Bedrock entity components (WP D). Goals are in `server/mob/goal/core`.
Both register themselves from `register.go`; link them with

```go
_ "github.com/df-mc/dragonfly/server/mob/component/core"
_ "github.com/df-mc/dragonfly/server/mob/goal/core"
```

Sources: BDS 1.21.3 dump (`AgeableComponent`, `BreedableComponent`,
`BreedGoal`, `InteractComponent`, `SpawnActorComponent`, `TimerComponent`,
`BreathableComponent` + breathe system, `ExperienceRewardComponent`,
`LegacyGoalDefinition::parse` for defaults), 1.26 JSON for the newer keys
(`offspring`, `pause/reset_growth_items`, `on_tempt_start/end`,
`chance_per_tick_to_float`).

## Components

| Key | What it does |
|---|---|
| `health` | max + start health (range allowed); records the cause of every hit (`LastHurtOf`, used by panic) |
| `movement`, `underwater_movement`, `lava_movement`, `flying_speed`, `follow_range`, `knockback_resistance`, `attack_damage`, `horse.jump_strength` | attribute `value` (number or range, rolled once) and `max` |
| `attack` | attack_damage + hit effect; `AttackOf(m)` rolls one hit for melee goals |
| `type_family`, `collision_box`, `scale` | families, Meta Width/Height, Meta Scale |
| `is_baby`, `is_sheared`, `is_saddled`, `is_shaking`, `is_tamed`, `is_chested`, `is_charged` | Meta flags |
| `variant`, `mark_variant`, `skin_id`, `color`, `color2` | Meta values; run-time changes (`SetColor`, `SetVariant`) are saved |
| `ageable` | grows up after `duration`, feeding speeds it up, pause/reset items, fires `grow_up` |
| `breedable` + `offspring` | breed items put adults in love (600 ticks), `CanMate`/`Mate` make the baby (`entity_born`, inherited properties, mixed sheep colour, `on_bred` xp), 60 s cooldown |
| `interact` | filters, cooldown, use/hurt/transform item, `spawn_items` loot, sounds, event (milk, shear, saddle, ignite) |
| `loot`, `equipment`, `experience_reward` | loot tables on death (+ furnace_smelt when burning, Looting from the killer's hands), spawn gear rolled on the first tick and put in its slot like BDS `Actor::equip`, slot drop chances; xp drops with the corpse 20 ticks after death |
| `spawn_entity` | timed item drops / spawns with filters (chicken eggs) |
| `timer`, `ambient_sound_interval`, `hurt_on_condition`, `breathable` | timers, ambient sounds, per-tick damage, air supply and drowning |
| `nameable`, `is_hidden_when_invisible`, `leashable`, `leashable_to`, `balloonable` | no-ops / stubs |

Equipment drops on death are mob-wide (`DropEquipmentOnDeath`, a
`mob.DeathHook`, BDS `Mob::dropEquipmentOnDeath`): 8.5% per slot unless
`slot_drop_chance`/`SetDropChance` says otherwise, +1% per Looting level, only
for player (or pet) kills unless the chance is 1; items a mob picked up
(`loot.MarkPickedUp`) always drop; the slots are emptied. Loot functions
`enchant_random_gear`, `enchant_randomly`, `enchant_with_levels`,
`specific_enchants`, `set_potion`, `set_banner_details` (ominous banner) and
the regional difficulty (`random_regional_difficulty_chance`) use package
`mob/component/loot`, which also has the Looting enchantment, the trident and
fishing rod items, the BDS special multiplier and the crossbow charge state.
Charged creepers make zombies, skeletons, wither skeletons, creepers and
piglins drop their heads.

Loot tables: `loot_tables/` holds the 120 tables the vanilla entities reference,
merged from the BDS 1.26.63 packs (newest wins). `AddLootTables(fsys)` adds
custom ones. Legacy item names (`muttonRaw`, `bucket:1`, `wool` + colour
data, `record_*`, ...) resolve to Dragonfly items in `items.go`.

`MobSound` (a `world.Sound`) and `EatGrassAction`/`LoveHeartsAction`/`FeedAction`
(world.EntityAction with `ActorEvent()`) are for the client packages.

## Not modelled

Leads (riding is in package mob, riding.go, and mob/component/ride),
the minecraft:home restriction in
RandomPos, game rules (mobGriefing, doMobLoot), in-love actor flag (hearts are
sent as actor events every 10 ticks).

## Tests

`coretest` is the test kit (hand-written definitions with groups/events, a
small filter compiler, fake navigation/look/jump). Tests cover attributes,
milking, shearing, eggs, loot/xp, breeding, growing up, timers, drowning,
Molang, item names, loot table loading and saved colours.

```
go vet ./server/mob/goal/core/... ./server/mob/component/core/... && go test -p 2 ./server/mob/goal/core/... ./server/mob/component/core/...
```
