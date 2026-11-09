# server/mob/control and server/mob/physics (WP C)

Controls and travel physics for definition-driven mobs, ported from the BDS
1.21.3 dump. Java/Paper only fills gaps, marked `source: java` in the code.

## control

| Piece | Registered as | What it does |
|---|---|---|
| `MoveControl` | `minecraft:movement.basic/generic/amphibious/sway/fly/hover/glide/skip/jump` | Wanted position -> facing yaw + `Mob.Input`. |
| `JumpControl` | `minecraft:jump.static` (`jump_power`, default 0.42), `minecraft:jump.dynamic` | Hands a requested jump to travel once (`Input.Jump`). |
| `LookControl`, `BodyControl` | init hook `control/defaults` (every mob) | Head yaw/pitch, body yaw. |

MoveControl (BDS `MoveControl::tick`, `calculateMoveSpeed`, `calculateYRotation`):
- Speed = movement attribute x multiplier. It ramps up by 10% of that a tick,
  slows down within 1 block of the end of the navigation path (never below
  half speed), and is capped at 0.25 going down. The wanted position counts as
  reached at distance² < 0.1 (unless it is 0.25 or more above).
- The facing turns at most `max_turn` (default 30) degrees a tick, up to twice
  that while speeding up. It does not turn within 0.05..0.25 blocks of the target.
- It jumps when the target is higher than `physics.MaxAutoStep` (0.009 in
  water) and the horizontal distance² < max(width, 1).
- Mobs without gravity: velocity += direction x 0.05 x multiplier a tick.
- `Strafe(f, r)`: one tick at 0.25 x speed (Java's strafe).
- Not ported exactly:
  - BDS keeps the old speed when there is no path; here the speed ramps up
    anyway, so goals that drive the control directly still move the mob.
  - `generic`/`amphibious`/`sway` swim in 3D in water (GenericMoveControl's
    swim branch), and `sway` does not walk.
  - `fly`/`hover`/`glide` follow Java's FlyingMoveControl, and hover keeps
    flying when idle.
  - `skip` hops whenever it walks; `jump` waits `jump_delay` seconds between
    jumps on the ground (Java's SlimeMoveControl).
  - jump.dynamic skip heights and the sway amplitude are not modelled.

LookControl:
- A look target lasts one tick.
- Head yaw and pitch turn at most yawMax/pitchMax a tick. Without a target
  they return to the body and to 0 at 10 a tick.
- The head is clamped to ±75° from the body.

BodyControl:
- Moving (horizontal velocity² > 2.5e-7): turns towards the facing yaw at
  ≤ 25° a tick.
- Standing: stays within 75° of the head. That allowance shrinks to 0 between
  ticks 10 and 20 of a still head.

## physics

`Travel` (installed with `mob.SetTravel`) does the following each tick:
1. Liquid state: the box is shrunk by 0.4 at the top and bottom (and 0.1
   sideways for lava). Underwater means the eyes are 1/9 below the surface.
2. Jump:
   - in a liquid: +0.04;
   - on the ground: the jump control's power plus 0.1 per Jump Boost level,
     then a 10-tick delay while the jump is held.
3. Acceleration from the input × 0.98:
   - ground: `Input.Speed × (0.546 / (blockFriction × 0.91))³`, with friction
     taken at `floor(minY - 0.1)`;
   - air: 0.02;
   - water: `underwater_movement`;
   - lava: `lava_movement`.
4. Climbing (`can_climb` on ladders, vines and the other climbable blocks;
   spiders through `navigation.climb`): speed is clamped to ±0.15 and falling
   to 0.15. After a horizontal collision the mob climbs at 0.2 with no gravity
   that tick. Spiders show `Meta.Climbing`.
5. The move:
   - collision is Y, then X, then Z, against `entity.BlockBBoxesAround`;
   - auto step up to `MaxAutoStep` (0.5625, or `variable_max_auto_step`
     base_value): up, across, then down, kept if it gets further.
6. Landing: the block's `EntityLand` runs (hay bale, slime, bed). The mob then
   takes `ceil(fallDistance - 3 - jumpBoost)` damage through `Mob.Hurt`, so
   damage_sensor sees it. Water and lava reset the fall distance.
7. Drag and gravity:
   - ground/air: gravity 0.08 (0.01 with slow falling), then vertical drag
     0.98; horizontal friction is `blockFriction × 0.91` on the ground and
     0.91 in the air;
   - water: horizontal drag `water_movement.drag_factor` (default 0.8),
     vertical drag 0.8, then gravity 0.02;
   - lava: drag 0.5 on all axes, then gravity 0.02;
   - liquid gravity is skipped for a mob whose navigation has
     `can_sink: false` and whose top and bottom are both out of air.
8. Climbing out of a liquid: at a wall, with free space 0.6 above → vy = 0.3.
9. Push (`Mob::pushActors`): pushable mobs in the box grown by 0.2 sideways
   are pushed apart at 0.05 × (1 - push_through) a tick. Players push mobs.
   An unpushable mob pushes others but does not move.

`KnockBack` (`mob.SetKnockBack`):
- `knockback_resistance` ≥ 1: no effect.
- Otherwise: velocity × 0.5, plus force sideways and height up, both scaled by
  (1 - resistance). Y is capped at height.
- Dragonfly's force and height (0.45 / 0.3608) are taken as the impulse.

Components: `physics` (has_gravity → `Meta.NoGravity`, has_collision),
`can_climb`, `pushable`, `pushable_by_entity` (presets ignored),
`push_through`, `water_movement`, `variable_max_auto_step`,
`block_movement_slowdown_immunity` (cobwebs: velocity restored after the
built-in environment step).

Helpers for other packages:
- `MaxAutoStep`, `HasGravity`, `HasCollision`, `CanClimb`, `Pushable`
- `SetFlying` / `Flying`
- `AppliesLiquidGravity`
- `Climbable(block)`

Not modelled:
- soul sand, honey and berry-bush slowdown
- honey jump reduction
- riding
- depth strider
- sprinting
- `push_towards_closest_space`
- piston pushing (`pushable_by_block`)

Tests (headless worlds):
- `go test -p 2 ./server/mob/control/... ./server/mob/physics/...`
- Speeds measured: 0.2 → 1.73, 0.23 → 2.28, 0.25 → 2.70, 0.3 → 3.89 blocks/s.
- A knockback moves a mob 2.15 blocks (0.84 at resistance 0.5).
