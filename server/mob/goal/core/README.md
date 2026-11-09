# server/mob/goal/core

Passive Bedrock goals (WP D), ported from the BDS 1.21.3 goals named below.
They move mobs only through `m.Navigation()`, `m.LookControl()` and
`m.JumpControl()`; components they need (breedable, health's hurt record) are
in `server/mob/component/core`, which documents both packages.

| Key | BDS | Defaults (JSON) | Flags |
|---|---|---|---|
| `random_stroll` | RandomStrollGoal | speed 1, xz_dist 10, y_dist 7, interval 120 | move, look |
| `look_at_player` / `look_at_entity` | LookAtActorGoal | look_distance 8, probability 0.02, look_time 2-4 s, FOV 360 (player filter for look_at_player) | look |
| `random_look_around` | RandomLookAroundGoal | probability 0.02, look_time 2-4 (ticks, as BDS passes it), angles -180..180 | move, look |
| `float` | FloatGoal | jump 80% of ticks in water/lava | jump |
| `panic` | PanicGoal | speed 1; within 40 ticks of a hit by a mob or one of damage_sources; flees 5/4 blocks away from the attacker; prefer_water | move |
| `tempt` | TemptGoal | speed 1, within_radius 16, repath 8-11 ticks, stop at widths/2+1.5, 100-tick cooldown, can_get_scared | move, look |
| `breed` | BreedGoal | speed 1, partner within 8, mates after 60 ticks when close | move, look |
| `follow_parent` | FollowParentGoal | speed 1, adult of the same type 3-16 blocks away, repath every 10 ticks | none |
| `eat_block` | EatBlockGoal | success_chance (Molang), time_until_eat 1.8 s, block in or under the mob | move, look, jump |
| `mount_pathing` | MountPathingGoal | speed 1, target_dist 0, track_target; carries a hostile rider to its target, repath every 4-10 ticks | move, look |

`RandomPos` is BDS RandomPos::generateRandomPos (10 tries, snap to ground,
best walk target value: animals like grass and light, monsters the dark).
The default panic causes are the documented list (BDS keeps them in static
data the dump lacks).
