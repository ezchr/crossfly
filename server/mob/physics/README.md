# server/mob/physics

This package is the mob travel step: ground, air, water and lava movement, collision, auto step, climbing, pushing, fall damage and knockback. It also handles the components `physics`, `can_climb`, `pushable`, `pushable_by_entity`, `push_through`, `water_movement`, `variable_max_auto_step` and `block_movement_slowdown_immunity`.

`../control/README.md` documents this package together with the controls. That covers the per-tick order, the constants, their BDS sources, what is not modelled, and the measured speeds.
