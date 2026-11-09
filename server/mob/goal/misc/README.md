# server/mob/goal/misc + server/mob/component/misc

Combat extras, damage/effect rules, triggers and sensors, and the remaining
animal, illager, golem and spell goals. Linked with one line in
`vanilla/vanilla.go` (`goal/misc`, which imports `component/misc` through
`kit` and blank-imports the sub-packages `animal`, `social`, `hostile`).
Sources: BDS 1.21.3 decompile (class names in each doc comment), the 1.26.63
JSON, `source: java/paper` where the dump has nothing.

## Packages

| Package | Keys |
|---|---|
| `component/misc` | fire_immune, freezing_immune, freezing_vulnerable, mob_effect_immunity, cannot_be_attacked, mob_effect, spell_effects, area_attack, damage_over_time, attack_cooldown, apply_knockback_rules, combat_regeneration, on_hurt, on_hurt_by_player, on_death, on_friendly_anger, on_equipment_changed, entity_sensor, block_sensor, genetics, strength, is_stunned, is_ignited, is_pregnant, dimension_bound |
| `goal/misc` | timer_flag_1/2/3, ram_attack, charge_attack, knockback_roar, move_to_block, stomp_turtle_egg, avoid_block, move_to_random_block, move_towards_target, target_when_pushed, hold_ground, follow_mob, look_at_target, send_event, summon_entity |
| `goal/misc/animal` | fox: stalk_and_pounce_on_target, nap, find_cover, eat_carried_item, random_look_around_and_sit, raid_garden, defend_trusted_target; panda: roll, sneeze, snacking, lay_down, scared, random_sitting; frog: croak, eat_mob, jump_to_block, lay_egg; sniffer: random_search_and_dig; silverfish_merge_with_stone, silverfish_wake_up_friends |
| `goal/misc/social` | go_and_give_items_to_owner/noteblock, stay_near_noteblock, drop_item_for, make_love, receive_love, play, behavior.hide, sniff, jump_around_target, move_around_target, fire_at_target |
| `goal/misc/hostile` | teleport, looked_at, break_blocks, ravager_blocked, custom_hit_test, group_size, reflect_projectiles |
| `goal/combat/variants_misc.go` | nearest_prioritized_attackable_target, delayed_attack, stomp_attack (they reuse goal/combat's target and melee bases) |
| `goal/misc/kit` | shared helpers (ranges, triggers, entity/block search, movement, loot, actor events); registers nothing |

Not here because other packages own them: minecraft:scheduler and
minecraft:hide (component/village), behavior.charge_held_item (goal/equip),
trust/giveable (component/tame), take_block/place_block (goal/village).

## Hooks this work added outside its packages

- `mob.ActorFlags` (`Meta.ActorFlags`, `Mob.SetActorFlag/ActorFlag/ExtraActorFlags`,
  actor_flags.go): extra Bedrock actor flags (timer_flag_N, ram_attack,
  delayed_attack, standing, roaring, casting, charging, stunned, rolling,
  sneezing, ...). server/session sets every one (flags >= 64 in FlagsTwo);
  javasession maps fox, panda, vex and polar bear ones (mobdata_misc.go) and
  the boss/breeze/warden poses (mobdata_boss.go, another agent).
- `mob.EffectImmunity`: `Mob.AddEffect` skips effects a component is immune to.
- `sense.KnockbackApplier`: `combat.DoHurtTarget` lets apply_knockback_rules
  replace the 0.4/0.4 knock back.
- `misc.BlockBroken(tx, pos, block, breaker)`: Dragonfly has no world block
  break event; call it from block break handling to drive block_sensor.

## Notes

- Dragonfly's `Tx.EntitiesWithin` tests positions; area_attack, mob_effect,
  entity_sensor and fang bites test box intersection like BDS
  (`fetchEntities`) by growing the query.
- Player hurt immunity is wall-clock based in Dragonfly, so tests that need
  repeated hits use mob victims.
- Evocation fangs are not a Dragonfly entity: a fang is its bite (6 damage
  8 ticks after it would appear) without a visible fang.
- Freezing damage only comes from code using the "freezing" cause (no powder
  snow in Dragonfly). Turtle eggs, frog spawn, sweet berries do not exist, so
  stomp_turtle_egg, egg placing and berry raiding never find their blocks.

## Tests

`go test -p 1 ./server/mob/component/misc/ ./server/mob/goal/misc/... ./server/mob/goal/combat/`
(external test packages with `internal/mobtest` and the vanilla mobs; each
package has a BDS-defaults test and behaviour scenarios).
