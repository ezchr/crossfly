# Hostile mob AI (WP E)

Packages:

- `server/mob/goal/combat`: the hostile `minecraft:behavior.*` goals.
- `server/mob/component/combat`: the hostile components.
- `server/mob/component/combat/sense`: shared helpers (line of sight with the
  BDS per-tick sight cache, field of view, daylight, difficulty, event
  triggers, value ranges, damage cause names). It registers nothing.

Link both with the two blank imports in `server/mob/vanilla/vanilla.go`.
Sources are the BDS 1.21.3 decompile (`_Z_global/<Class>.cpp`); field offsets
are noted next to the Go fields. Code marked `source: java/paper` is not in
the dump and follows Java 26.3.

## Goals

| Key | BDS class | Notes |
|---|---|---|
| `nearest_attackable_target` | NearestAttackableTargetGoal, TargetGoal | Scan every `scan_interval` ticks (offset by the mob's UUID) or with a 1/`attack_interval` chance per tick. Candidates are sorted by distance; the first that passes `entity_types` (`max_dist`, `must_see` with forget time, filters with the candidate as `other`), descriptor cooldown, and the visibility limits (sneak x0.8, invisible x0.7 x armour cover, `max_height`) is the target. `reselect_targets`, `must_reach`, `persist_time`, `attack_owner`, `set_persistent`. Creative/spectator players and peaceful difficulty are never targets. Losing sight longer than `must_see_forget_duration`, leaving follow range, or dying drops the target. |
| `hurt_by_target` | HurtByTargetGoal | Targets the attacker of the last 100 ticks if it passes `entity_types` (default `max_dist` 16). `alert_same_type` alerts mobs of the same type within follow range (±10 y) that have no target. `hurt_owner`. |
| `melee_attack` | MeleeAttackGoal + MeleeAttackBaseGoal | Reach² = (max(width,0.5) x `reach_multiplier`)² + target width, horizontally with vertical overlap or in 3D. Hits need line of sight from the eyes and the target within `melee_fov` of the head. Path recalculation every `min_path_time`..`max_path_time`, plus `inner/outer_boundary_time_increase` beyond 16/32 blocks, plus `path_fail_time_increase` when no path. `cooldown_time` (first hit after one cooldown), `attack_once`, `random_stop_interval`, `track_target`, `require_complete_path`, `on_attack`/`on_kill`, `can_spread_on_fire` (difficulty chance 0/0.42/1.14/1.92), `attack_types` (a family). |
| `melee_box_attack` | MeleeBoxAttackGoal | Same, but hits when the target's box touches the mob's box grown by `horizontal_reach` (0.8) on x/z. |
| `ranged_attack` | RangedAttackGoal | Walks to the target until within `attack_radius` (or `attack_range.max`) and seen for `target_in_sight_time`, then stops and faces it; backs off inside `attack_radius_min`. Fires through `minecraft:shooter` every `attack_interval` (scaled by distance between min and max), in bursts of `burst_shots`, within `ranged_fov`. Crossbow charging (`charge_*`) is not done. |
| `swell` | SwellGoal | Stops the creeper; `Meta.Swell` is 1 while the fuse is lit and the target is within `stop_distance` (box gap) and seen, else -1. The explosion itself is `minecraft:explode`, added by the `start_exploding` event of `target_nearby_sensor`. |
| `leap_at_target` | LeapAtTargetGoal | 1 in 5 per tick at 2..4 blocks: 0.4 towards the target + 1.2 x horizontal motion, `yd` up; runs until landing. |
| `flee_sun` | FleeSunGoal / FindCoverGoal | Day, burning, under open sky: 10 random tries within 10 blocks (±3 y) for a spot without sky light and brightness > 0.5. |
| `avoid_mob_type` | AvoidMobTypeGoal | Nearest living match within `max_dist` (per entry), a random standable spot 16/7 blocks away in the opposite half, walk speed, sprint within 7 blocks, keeps going while within `max_flee`. |
| `pickup_items` | (basic) | Walks to item entities within `max_dist` that are better armour, a better weapon, or anything for an empty hand (`can_pickup_any_item`), honours `excluded_items` names, picks up within `goal_radius` and drops the replaced item. `pickup_based_on_chance` rolls once per mob (0 / 0 / 27.5 % / 55 % by difficulty, an estimate). |
| `equip_item`, `use_kinetic_weapon` | stubs | Never run (picking up equips; spear charging is not ported). |

Target goals use control flag 1 (move) like BDS: it only competes with
other target goals.

## Components

| Key | Notes |
|---|---|
| `shooter` | `def`, `aux_val` (tipped arrows), `power`, `projectiles` (chance, filters, `lose_target`), `sound` "bow". Arrows (no pickup, gravity 0.05, damage 2 x speed), snowballs, eggs, splash/lingering potions, ender pearls. Aimed at a third of the target's height, +0.2 per block of horizontal distance for arrows, uncertainty 16 - 4 x difficulty (mob_arrow). Other projectiles log once and do not fire. |
| `explode` | `fuse_lit` lights on attach (rolled `fuse_length`), counts down, explodes with `power`, `causes_fire`, `breaks_blocks`, `destroy/fire_affected_by_griefing` (see `MobGriefing`), `allow_underwater`, then removes the mob. `Meta.Ignited`/`FuseTicks`; fuse saved as `Fuse`/`IsFuseLit`. `Ignite` for flint and steel. |
| `burns_in_daylight` | Day, brightness > 0.5, chance (b-0.4)x2/30 per tick, sky visible from the eyes, not in water or rain: 8 s of fire, or 0-1 helmet durability instead. |
| `environment_sensor` | Every trigger whose filters pass fires every tick. |
| `damage_sensor` | `cause` (BDS names, `all`), `on_damage` (filters with the attacker as `other`/`damager`, event), `deals_damage` (false / "no" / "no_but_side_effects_apply" cancel), `damage_multiplier`, `damage_modifier`. Lightning: the nearby bolt is passed as `other`. |
| `target_nearby_sensor` | `on_inside_range` once on entering `inside_range` (seen, with `must_see`), `on_vision_lost_inside_range`, `on_outside_range` once beyond `outside_range`. |
| `on_target_acquired` / `on_target_escape` | On any target change to a new target / to none. |
| `angry` | Angry (`Meta.Angry`) while the target passes `filters`; `duration` ± `duration_delta` s restarted by each attack; then `calm_event` and the target is dropped. `broadcast_anger`, `_on_attack`, `_on_being_attacked`, `broadcast_range`, `broadcast_targets`, `broadcast_filters`. Timer saved. |
| `transformation` | `into` "id<event>", `delay` (number or `{value}`), `preserve_equipment`, `drop_equipment`, `keep_owner`; name tag, persistence, fire carry over; the new mob gets the event (or `entity_transformed`), then `entity_transformed` with the old mob as `other`. `Meta.Converting` during the delay. |
| `annotation.break_door` | Breaks a closed wooden door in front of a pathing/blocked mob after `break_time` (12 s) from `min_difficulty` (hard). Navigation should treat closed doors as passable when the mob has this component. |
| `equip_item` | `excluded_items` (read by nothing yet; pickup_items has its own list). |

Event-firing components run as `StageSensors` systems (`combat/*`), not as
component tickers.

## Gaps / needs elsewhere

- Mobs are not damaged by explosions: `mob` core `state.Explode` only pushes.
- Dragonfly explosions cannot leave blocks alone; `breaks_blocks: false` and
  `MobGriefing` off use `ExplodeEntitiesOnly` (own entity pass).
- Navigation `avoid_sun` (melee canUse) and mob item pick-up as an
  `entity.Collector` are not modelled.
- Loot-table equipment is `minecraft:equipment` (WP D).
