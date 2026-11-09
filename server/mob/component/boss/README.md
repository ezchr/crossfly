# server/mob/component/boss, server/mob/goal/boss

Boss and special hostile mobs: ender dragon, wither, warden, and the
components they share with others. `goal/boss` holds the goals and imports
`component/boss`; `vanilla/vanilla.go` links `goal/boss`.

Sources: BDS 1.21.3 decompile (`_Z_global/<Class>.cpp`, named below), the
1.26.63 entity JSON, and Java 26.x where the dump lost a value (marked
`source: java/paper` in the code).

## Components

| Key | BDS | Notes |
|---|---|---|
| `boss` | BossComponent, BossSystem | Bar for viewers within `hud_range` (55), checked about once a second; health updates when it changes; hidden out of range, on detach and 10 ticks after death. `name` or the type's localisation key; colour 5 (wither 6); `should_darken_sky`. Viewers are the sessions implementing `BossBarViewer` (one bar per boss). Also carries the dragon's and the wither's hard-coded damage rules and saved state. |
| `on_start_landing`, `on_start_takeoff` | OnStart*Definition | Triggers the dragon goals fire (`FireTrigger`). |
| `anger_level` | AngerLevelComponent, AngerLevelSystem | Anger per entity (`nuisance_filter`, players first in the order), `max_anger` cap, -1 per `anger_decrement_interval`, `on_increase_sounds` (Molang), `remove_targets_below_angry_threshold`, `query.anger_level` (def and core Molang read `AngerLevel()`). Warden: a hit gives threshold+boost and the target (Warden::_hurt), no damage while digging/emerging, a new target delays the sonic boom 200 ticks. `IncreaseAngerAt` is what `goal/misc` sniff calls. |
| `suspect_tracking` | SuspectTrackingComponent | Suspicious position + its tick. |
| `vibration_listener` | VibrationListener, WardenVibrationConfig | Range 16 from the head, 40-tick cooldown, closest-per-tick selection (frequency on ties), travel time = distance in ticks, wool damps steps and blocks the line, sneaking/`vibration_damper` sources ignored, not while digging/emerging. Sources Dragonfly can see: entity steps, swimming, landing, flaps (game_event_movement_tracking), projectiles shot/landed, mobs hurt (HurtHook). Warden reaction: actor event 77, anger at the source (35) or a projectile's shooter (10, or 35 for a second shot within 5 s), suspicious position while not angry. |
| `vibration_damper` | IsVibrationDamperFlag | Flag read by the listener. |
| `game_event_movement_tracking` | GameEventMovementTrackingComponent/System | emit_move/emit_swim (default on) let the listener hear the mob's steps and swims; emit_flap (default off) sends a flap (GameEvent 27, frequency 1) every 10 ticks while in the air (not on the ground, climbable, scaffolding, powder snow or cobweb, not in a liquid) to the listeners of the world. No sculk blocks exist, so the warden is the only listener. |
| `heartbeat` | HeartbeatServerSystem | Molang `interval` -> actor data 125 (ticks), sound 376 -> 126; the client plays it. |
| `movement_sound_distance_offset` | | `value` (1) -> actor data 124. |
| `instant_despawn` | InstantDespawnSystem | Removed on the next tick (creaking crumbling). |
| `body_rotation_blocked` | | Body yaw frozen while active. |

## Hard-coded actors

- **Ender dragon** (`dragon.go`, EnderDragon::postAiStep): steering to
  `TargetPos` with the BDS formula (air speed caps the climb, turn speed, 0.06
  thrust, 0.91/0.875 drag), flight history, body part boxes (head, neck, body,
  wings), wing knock back (5 damage while flying), head/neck bites (10), block
  breaking with the immune list (mobGriefing), crystal healing (nearest within
  32, 1 per 10 ticks; a destroyed healing crystal deals 10 to the head and
  sends the dragon after the nearest player), the 24-node graph (rings
  60/40/20, adjacency masks, A*), damage rules (only players and explosions,
  min(d,1)+d/4 off the head/neck, arrows burn on a perched dragon, a perched
  dragon takes off after 25% damage, lethal -> 1 health + on_death), the
  death animation (200 ticks rising, explosions, 12000/500 xp in bursts).
- **Wither** (`wither.go`, WitherBoss): 200-tick spawn charge from 1 health
  (heals ceil(max/200)/tick, max health 50/75/100% by difficulty, explosion
  7), hovering 5 above the target (phase 1) or level with it (phase 2), three
  heads (side heads pick random players within 30x8x30, 10-20 tick checks,
  black skulls 40-60 ticks apart on normal/hard, random blue skulls when idle),
  main head every 20 ticks with every 4th skull blue, fire rate halving per
  sixth of health lost, half health: armour (projectiles bounce), dive and
  explosion 7, 80-tick pause, 3 wither skeletons, 20-tick wind-up dashes of
  10 ticks (15 damage, breaks blocks), blocks broken 20 ticks after a hit,
  regeneration 1/s without players within 50, death animation 200 frames then
  explosion 7. Actor data 48, 49-51, 52, 105. `TrySpawnWither(tx, pos)` checks
  the soul sand T + skulls (a server calls it from its block placement
  handler).
- **Warden** (`warden.go`): sonic boom cooldowns (melee hit +100, new target
  +200), being pushed by a nuisance (anger + suspicion, once a second).

## Projectiles (`projectiles.go`)

Entity types with `entity.ProjectileBehaviour`, registered with
`combat.RegisterProjectile` (so `minecraft:shooter` and
`combat.ShootProjectile` fire them) and listed by `ProjectileTypes()` for a
server's entity registry. Not saved.

| Id | Power | Hit |
|---|---|---|
| `wither_skull` | 1.2 | explosion 1 (blocks with mobGriefing), Wither II 10 s normal / 40 s hard |
| `wither_skull_dangerous` | 0.6 | the same (BDS also lets it break obsidian; not modelled) |
| `dragon_fireball` | 1.3, uncertainty 10 | harming cloud, radius 6, 6 s |
| `breeze_wind_charge_projectile` | 0.7, uncertainty 5-4d | 1 damage, wind burst radius 3 x 0.6 (no damage) |

## Goals (`goal/boss`)

| Key | BDS | Defaults |
|---|---|---|
| `dragonholdingpattern` | DragonHoldingPatternGoal | ring paths; 1/(crystals+3) land, else target the nearest visible player within 64 of the podium |
| `dragonstrafeplayer` | DragonStrafePlayerGoal | active 1, flight 0.6, turn 0.7, zone 10-150, switch 0.125, in view 0.25 s, range 64, view angle 10 |
| `dragonchargeplayer` | DragonChargePlayerGoal | active 3, continue 0.5 s |
| `dragontakeoff` | DragonTakeoffGoal | node 40 behind the head, ends 10 from the podium |
| `dragonlanding` | DragonLandingGoal | path to the node facing the nearest player, final approach at 1.5 with full turning, sits within 1 |
| `dragonscanning` | DragonScanningGoal | player within 20: target after 25 ticks; none for 100: take off (charge a visible player within 150) |
| `dragonflaming` | DragonFlamingGoal | 4 flames, cooldown 10 s, flame 0.5 s, roar 2 s, smoke 10 s, radius 5 |
| `dragondeath` | DragonDeathGoal | to the podium at 3, then the death animation |
| `wither_random_attack_pos_goal` | WitherRandomAttackPosGoal | 10 blocks from the target in a random direction (phase 1) |
| `wither_target_highest_damage` | WitherTargetHighestDamage | player with the most damage within 70 |
| `sonic_boom` | SonicBoomGoal | duration 0, speed 1, damage 30, cooldown 5 s, range 15 h / 20 v, sound at 1.7 s, knock back 0 |
| `roar` | RoarGoal | duration 0; top nuisance past the threshold, then target + angry_boost |
| `dig` | DigGoal | duration 0, idle_time 0, disturbances off; always removes the mob when it ends |
| `emerge` | EmergeGoal | duration 5 s, cooldown 0.5 s, on_done |
| `investigate_suspicious_location` | InvestigateSuspiciousLocationGoal | speed 1, goal_radius 1.5 |

## Clients

- Bedrock: `mob.Mob.SetActorData` (package mob `actor_data.go`) for raw actor
  data, read by `session/mob_view.go`; boss bars in `session/mob_boss.go`
  (BossEvent keyed by the boss's runtime id); plain actor events
  (`ActorEvent()` actions) in `session/world.go`.
- Java: `javasession/mobdata_boss.go`: per-boss bars (entity UUID; dragon pink
  with music and fog, darken for the wither), wither data 16-19, dragon phase,
  warden anger, poses for warden/breeze actor flags (`self.go`), the elder
  guardian ghost (game event 10), tendril shiver (entity event 61).

## Not modelled

The End fight (exit portal, gateways, egg, respawn), dragon parts as separate
hit boxes (hits are classed by the attacker's line of sight), wither skull
obsidian breaking, wind charge block toggling and projectile reflection,
vibration particles (LevelEvent 2027), the Java sonic-boom attack animation,
and saved anger lists.
