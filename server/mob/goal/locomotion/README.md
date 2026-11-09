# server/mob/goal/locomotion and server/mob/component/locomotion

Water mobs, flying mobs, slimes and squids. The goals are in this package and the components and hard-coded actor behaviour are in `component/locomotion`, which this package imports. `vanilla/vanilla.go` links both through one import of this package.

Sources:
- BDS 1.21.3 dump, one class per goal (named below), `LegacyGoalDefinition::parse` and the `*Definition` constructors for defaults.
- For keys that are not in 1.21.3 (`aquatic_charge_attack`, `float_tempt`, `air_drag_modifier`, `uses_uniform_air_drag`, `bounciness`, `uses_legacy_friction`, `can_stand_on_powder_snow`, `pushable_by_block`): the 1.26.63 JSON, with Java behaviour where needed.

## Goals

| Key | BDS | Defaults | Flags |
|---|---|---|---|
| `random_swim` | RandomSwimmingGoal | speed 1, xz 10, y 7, interval 120, avoid_surface true; height re-rolled between floor+0.5 and surface-1.5 (30 tries), pitch over 50 degrees refused | move, look |
| `swim_idle` | SwimIdleGoal | idle_time 5 s, success_rate 0.1; holds the flock heading | move |
| `swim_wander` | SwimWanderGoal | interval 0.01, look_ahead 5, speed 1, wander_time 5 s; ray steering, 12 degree turns, underwater speed | move |
| `move_to_water`, `move_to_liquid`, `move_to_land` | MoveToLiquidGoal, MoveToLandGoal (BaseMoveToBlockGoal) | speed 1, search_range 0, search_height 1, search_count 10, goal_radius 0.5, material Any; every 200-399 ticks; 1200 tick travel/stay caps, 8 s cooldown on a bad path | move, look |
| `rise_to_liquid_level` | RiseToLiquidLevelGoal | liquid_y_offset, rise_delta, sink_delta 0; 0.4 dead band | jump |
| `swim_up_for_breath` | BreatheAirGoal (hard-coded `breathe_air` in 1.21.3) | speed 1.4, radius 4; air < max(5 x depth, 150) | move, look |
| `swim_with_entity` | SwimWithEntityGoal | success 0.1, stop chance 0.0333, check 0.5 s, catch up 12 / x2.5, match 2, speed 1.5, range 20, stop 5 | move, look |
| `random_breach` | RandomBreachingGoal (+ DolphinMoveControl breach) | speed 1, xz 10, y 7, interval 120, cooldown 0 | move, look |
| `find_underwater_treasure` | FindUnderwaterTreasureGoal | speed 1, search_range 0, stop_distance 2 | move |
| `play_dead` | PlayDeadGoal | duration 1 s, start chance 1, force_below_health 0, damage range 0..0, all causes; PLAYING_DEAD flag, regeneration | move, look |
| `float_wander` | FloatWanderGoal | xz 10, y 7, y_offset 0, must_reach false, random_reselect false, float_duration 0 | none |
| `random_hover` | RandomHoverGoal | speed 1, interval 120, xz 10, y 7, y_offset 0, hover_height 0 | move |
| `random_fly` | RandomFlyingGoal | speed 1, xz 10, y 7, can_land_on_trees true, 1/120 while active < 100 ticks | move, look |
| `float_tempt` | (1.26, TemptGoal for fliers) | speed 1, within_radius 16, stop_distance width/2+1.5, 100 tick cooldown | move, look |
| `circle_around_anchor` | CircleAroundAnchorGoal | radius_change 1, chances 0.004 / 0.002857, goal_radius 0.5, angle_change 15, radius 5..15 | move, look |
| `swoop_attack` | SwoopAttackGoal | damage_reach 0.2, speed 1, delay 10..20 s (the BDS double x20 is kept) | move, look |
| `slime_float`, `slime_keep_on_jumping`, `slime_random_direction`, `slime_attack` | Slime*Goal | jump chance 0.8, speed 1.2 / 1 / turn 360 every 2+0..3 s / rotations 10, tired 15 s | move+jump / move+jump / look / look |
| `squid_idle`, `squid_flee`, `squid_dive`, `squid_move_away_from_ground`, `squid_out_of_water` | Squid*Goal + Squid::postAiStep | movement vector t; velocity eases 22% a tick towards it in water | none |
| `guardian_attack` | GuardianAttackGoal | magic 1, hard +2, elder +2, rotations 90, min_distance 3; sound at tick 10, hit at 80 (elder 60) | move, look |
| `aquatic_charge_attack` | (1.26) | reach 0, cooldown 4 s, overshoot 1.5, speed 1.2, knockback 2, range 16 | move, look |

The squid movement system runs in StageTravel after travel. Hurt hooks record the last hit for play_dead and swoop_attack, and squirt squid ink: actor event 15 and the ink sound.

## Components and actor behaviour (component/locomotion)

| Key / class | What it does |
|---|---|
| `flocking` (FlockingComponent/System) | leaders, members, merging, the steering vector; adds the hard-coded `minecraft:follow_flock` goal (FollowFlockGoal) for cod 0.09, salmon 0.12, tropical fish 0.05, dolphin 0.1 |
| `buoyant` (BuoyancyComponent) | wave timer, float/resurface vertical speed, drag down on removal |
| `friction_modifier`, `air_drag_modifier`, `uses_uniform_air_drag`, `bounciness` | applied around travel: a system at the end of StageControls records the state, one after the built-in travel step changes the velocity |
| `drying_out_timer` | counts out of water and rain, the three events, saved; `RefillWater` for water bottles |
| `bribeable` | bribe_items (fish = cod), 2 s cooldown; feeds find_underwater_treasure |
| `can_fly` | CANFLY actor flag |
| `block_climber`, `can_stand_on_powder_snow` | no fall damage in/over powder snow (by name); the nav adapter reads them for powder snow paths |
| `uses_legacy_friction`, `pushable_by_block` | markers (the physics is the 1.21.3 friction; Dragonfly has no pistons) |
| Dolphin | DolphinMoveControl: a generic move control with max turn 10, because no definition has a movement component |
| FloatNavigation (`navigation.float`: ghast, bat) | flies (no gravity, air drag on every axis) unless resting; faces its velocity, or its target within 64 blocks |
| Fliers (navigation.fly / navigation.hover / can_fly) | a system swaps the movement.fly/hover control of package control (air speed 0.02 x flying_speed: bees 0.7 blocks/s) for `FlyControl`: movement.fly is BDS FlyMoveControl (0.05 x movement speed a tick towards the target; parrot about 4 blocks/s); movement.hover follows Java FlyingMoveControl (flying_speed x modifier, ground: movement speed) as direct movement, the velocity easing halfway to it a tick (bee 2.7, allay 1.8 blocks/s), braking x0.35/x0.95 when idle |
| Phantom | `GlideControl`, a port of GlideMoveControl, replaces the movement.glide control of package control |
| Slime / magma cube | splits on death into 2-4 of half the size (spawn_medium / spawn_small) |
| Guardian | MOVING flag while swimming, thorns 2 to melee attackers while still, flopping on land; elder: ELDER flag, Mining Fatigue III for 5 min every 100 ticks within 50 blocks (+ actor event 35) |

## Clients

- Bedrock: the actor flags (PLAYING_DEAD, MOVING, ELDER, CANFLY) go through `Mob.ExtraActorFlags`. Squid ink, the guardian sound and the elder curse are actor events. The guardian beam is the target actor data.
- Java (`javasession/mobdata_locomotion.go`): guardian moving and attack target (16, 17), pufferfish puff state (17, from the variant), axolotl playing dead (19).

## Tests

- `defaults_test.go` checks the BDS defaults.
- `scenario_test.go` and `scenario2_test.go` test each behaviour: fish swimming in water and flocking, squid swimming, fleeing and staying still on land, slime hops and splits, ghast float, bee hover, phantom circles and swoops, guardian beam timing, dolphin breathing, breaching and treasure, axolotl playing dead, turtle to water, strider rising in lava, happy ghast tempt, nautilus charge; `flight_test.go` measures 5 s of flight: bee 2.70, allay 1.80, parrot 3.79, bat 7.19 blocks/s.
- `component/locomotion/component_test.go` covers buoyancy, friction and bounce, dolphin bribe and drying out, the CANFLY flag and guardian spikes.
- `javasession/mobdata_locomotion_test.go` checks the Java entity data.

```
go vet ./server/mob/goal/locomotion/... ./server/mob/component/locomotion/...
go test -p 1 ./server/mob/goal/locomotion/... ./server/mob/component/locomotion/... ./server/mob/vanilla/...
```

## Not modelled / approximations

- swim_wander and the squid steering: the side-ray angle (45) and the steering distance (3) are not in the dump.
- swim_wander moves through the velocity, not Actor::move.
- random_breach: the leap (0.4 forward, jump power up) is done by the goal at the surface, not by the navigation's breach node.
- find_underwater_treasure: Dragonfly has no structures. `TreasureLocator` is nil by default.
- drying_out_timer: nothing calls `RefillWater` yet, because there is no water bottle splash hook.
- guardian_attack: Dragonfly has no "chained" flag. A player's hurt cooldown can swallow part of the magic hit that follows the melee part.
- happy ghast float_wander: the 1.26 keys navigate_around_surface, surface_xz/y_dist and use_home_position_restriction are ignored.
- The home restriction in random positions is not applied.
- Squid ink is not shown to Java players (it needs particle packets).
- Peaceful slime removal (Slime::preNormalTick) is not handled here.
