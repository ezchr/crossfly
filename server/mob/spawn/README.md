# server/mob/spawn

Bedrock natural spawning and despawning for the mob framework (WP F). It is a
port of BDS 1.21.3 `Spawner`, `MobSpawnRules`, `SpawnConditions`,
`ActorSpawnRuleBase::loadSpawnCondition`, `SlimeSpawnRules` and
`DespawnComponent`, driven by the 60 vanilla 1.26.63 spawn rule files in
`server/mob/data`.

```go
import "github.com/df-mc/dragonfly/server/mob/spawn"
```

The package registers itself (`minecraft:despawn`, `minecraft:persistent`
and the StageDespawn system `spawn/inactivity`); `server/mob/vanilla` links
it in.

## Turning spawning on

Natural spawning is **off in every world**. A server enables it per world:

```go
spawn.Enable(smpWorld, spawn.Config{}) // vanilla settings
spawn.Disable(smpWorld)                // off again
spawn.Enabled(w)                       // query
```

`Enable` starts a small ticker that runs `spawn.Tick` in the world every
50 ms (one `World.Do` per tick, waiting for the previous one). With
`Config.ManualTick` no ticker is started and the caller runs
`spawn.Tick(tx)` once per world tick (tests; a server with a tick hook).

`Config` (zero value = vanilla):

| Field | Default | Meaning |
|---|---|---|
| `SimulationDistance` | 4 | chunks around players that are spawned in. 4: mobs spawn at most 44 blocks away (`LOW_END_MAX_SPAWN_DISTANCE`), more: up to the rule's max (128) but not in edge chunks, below 4: nothing (BDS). |
| `MaxAttemptsPerTick` | 8 | budget: columns tried per tick. Every ticked chunk rolls 11/2000 per tick (BDS); rolls over the budget are dropped. |
| `GlobalCap` | 200 | spawnable mobs ticked in the world before spawning stops |
| `Caps` | `DefaultCaps` | per population_control cap override |
| `Categories` | all | only spawn these pools, e.g. `[]string{"animal"}` |
| `DisabledMobEvents` | none | e.g. `"minecraft:pillager_patrols_event"` |
| `ManualTick`, `Seed` | | see above / deterministic tests |

Despawning runs in every world by the mobs' own `minecraft:despawn`
component; `spawn.SetDespawning(w, false)` turns it off per world.

## What is implemented

Spawning (per tick, per world):

- Players: alive, not spectators. Ticked chunks: a circle of
  `SimulationDistance` chunks around each player; only loaded chunks
  (`Tx.BlockLoaded`), never loading or generating one.
- Per chunk: `nextInt(2000) <= 10` -> a random column. The first floor under
  the sky is the surface (`findNextSpawnBlockUnder`, surface mode: fails on
  solid blocks above, carpets and bottom slabs; leaves, glass, fences are
  passed through), every floor below it is underground (caves).
  Floors: solid top face, ice, top slabs; not glass/leaves.
- Conditions: `inWater` (two water blocks above the floor on the surface,
  one underground), `inLava`, surface, underground, brightness
  (`max(sky - skyDarken, block)`, sky darkening from time of day, rain and
  thunder as `Dimension::tick`; `adjust_for_weather` uses a darkening of 10
  in thunderstorms), moon brightness, height, difficulty, world age,
  delay filter, mob events.
- Rule choice: the biome's rules (biome filter evaluated once per biome)
  that pass `canSpawnInConditions`, weighted by `minecraft:weight`
  (`getMobToSpawn`), then a random herd, `rarity`, herd size
  `min + round((max-min) * f^2)`, `isSpawnPositionOk` (player distance
  `distance_filter` 24..128 / 44, `spawns_on_block_filter`,
  `spawns_on_block_prevented_filter`, `spawns_above_block_filter`, water/lava
  must match `spawns_underwater`/`spawns_lava`, no full blocks in the two
  cells, not on bedrock/barrier), `delay_filter` chance.
- Caps: global (200 ticked spawnable mobs), `density_limit` per identifier
  and the category caps of BDS, all counted in the 9x9 chunks around the
  attempt, surface and underground apart:

  | pool | surface | underground |
  |---|---|---|
  | animal | 4 | 4 |
  | monster | 8 (End 10) | 16 |
  | water_animal | 40 | 40 |
  | ambient | 0 | 2 |
  | cat | 4 | 0 |
  | pillager | 8 | 8 |

- Spawn: every herd member at the floor block's centre (+0.49) on top of it
  (BDS stacks herds; physics pushes them apart), after `Mob::checkSpawnRules`
  (no block collision for the base collision box, no magma/campfire unless
  fire immune). `permute_type` (zombie -> zombie_villager_v2, pillager
  patrol `id<event>`), `spawn_event`, herd `initial_event`/`event`,
  `is_persistent`. Identifiers without a registered mob type are skipped.
- Slimes: the hardcoded rule (slime chunks below y 40 via MT19937; swamps
  51..69 by moon phase and darkness).
- Biome tags: `world.Biome.Tags()`. Dragonfly's tags were compared with all
  89 biome definitions of the 1.26.50 vanilla pack and are identical.
  `is_snow_covered` = biome temperature < 0.15.

Despawning (`minecraft:despawn`, every tick in StageDespawn, like
`DespawnSystem`):

- nothing despawns while no player is in the world, or when the mob is
  persistent or has a name tag (named mobs are made persistent);
- `filters`: when the 3x3 chunks are loaded and the filter (package
  mob/filter) passes;
- otherwise `_tryStandardDespawnRules`: inactive for
  `min_range_inactivity_timer` (30 s) then 1 in `min_range_random_chance`
  (800) per tick, only with no player within `min_distance` (32; a player
  that close resets the inactivity timer); instantly with no player within
  `max_distance` capped at 44 (simulation distance 4), or at the simulation
  edge / beyond `max_distance` (128) with larger simulation distances.
- The inactivity timer counts up every tick and resets when hurt or when a
  player is within min_distance: `spawn.InactivityTicks(m)`.
- `minecraft:persistent` marks the mob persistent while attached.

## Not implemented / differences

- Structure spawns (`_tickSpawnStructureMobs`: witch huts, fortresses,
  monuments, outposts) and village spawns (cats, iron golems): blaze,
  wither skeleton, cat and pillager have no conditions for that reason.
- `player_in_village_filter` always passes (no villages).
- Under trees BDS keeps the surface flag while the floors are leaves; leaves
  are never floors here, so cave floors are always underground.
- The despawn distance at simulation distance 4 (44 blocks) is inferred: the
  dump passes it in a register the decompiler does not show.
- `remove_child_entities` (leashed mobs) is not applied.
- The global cap is per world (BDS: per level).
- `guaranteed_count` of permute_type is parsed but unused (no vanilla rule
  sets it).

## Tests

`go test -p 2 ./server/mob/spawn/...`: rule parsing (all 60 files), biome
filters, sky darkening, slime chunks, cows in herds on grass at noon in
plains 24..44 blocks away, zombies at night and not at noon or on peaceful,
zombies in the dark under a roof by day, category/density caps, the global
cap, nothing spawning while disabled, despawn by distance and inactivity,
name tag and `persistent`, despawning off and no players.
