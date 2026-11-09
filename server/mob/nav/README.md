# server/mob/nav

Pathfinding and navigation for vanilla mobs, ported from the Bedrock Dedicated Server 1.21.3
(decompiled `PathFinder`, `PathfinderNode`, `BinaryHeap`, `Path`, `PathNavigation` and subclasses,
`NavigationComponent`, `NavigationDescription`, `PreferredPathDescription`, `NavigationUtility`).
It has no dependency on the rest of `server/mob`; the mob type plugs in through the small
interfaces in `iface.go`.

## Using it

```go
// Once, from the entity definition:
nv, ok, err := nav.FromComponent("minecraft:navigation.walk", raw) // or nav.New(nav.KindWalk, opts)
nv.Traits = nav.Traits{StepHeight: 0.5625, FollowRange: 35, DoorBreaker: false}
pp, _ := nav.ParsePreferredPath(rawPreferredPath) // optional minecraft:preferred_path
nv.Traits.Preferred = &pp

// From goals (tx is the *world.Tx of the tick, m the mob):
nv.MoveTo(tx, m, pos, speedMultiplier)      // walk to the block containing pos
nv.MoveToEntity(tx, m, target, speed)       // walk to an entity (feet position)
nv.MoveAlong(tx, m, path, speed)            // follow a path made with CreatePath
p := nv.CreatePath(tx, m, pos)              // search only (reachability checks)
nv.Stop(m)
nv.Done(); nv.Pending(); nv.Path()

// Once per mob tick, after the goal selectors and before the move control:
nv.Tick(tx, m)
```

`Tick` calls `m.SetWantedPosition(waypoint, speed)` with the next waypoint, which is what
`MoveControl::setWantedPosition` receives in BDS. Wall climbers (`navigation.climb`) also call
`SetClimbing` on agents that implement `nav.Climber`.

### What the mob must provide (`iface.go`)

- `Agent`: `Position` (feet), `Size` (collision box width/height), `OnGround`, `InWater`, `InLava`,
  `HorizontalCollision`, `SetWantedPosition`, `ClearWantedPosition`.
- Optional `Climber` (`SetClimbing`) and `TraitSource` (`PathTraits()`, for traits that change at run
  time: door breaking by difficulty, passing fire with a target).
- `BlockSource`: `Block(pos)` and `Liquid(pos)`. `*world.Tx` satisfies it and also gives the height
  range, `HighestLightBlocker` (avoid_sun), `CurrentTick` (budget) and `World` (time of day, budget).

`Traits` holds the per-actor facts BDS reads from the actor rather than the navigation component:
max jump (`max(StepHeight, 1.2)`), follow range (search radius, measured from the goal as in BDS),
fire immunity, door opener/breaker annotations, powder snow / scaffolding climbing, breakable
blocks (break_blocks + mobGriefing) and the preferred path costs.

### Lower level

`nav.NewFinder().FindPath(src, req, goal, maxDist)` and `FindPathToBlock` run one search for a
`Request` (position, size, walker/swimmer/flier, options, traits) without a Navigation. `Path` has
the BDS accessors (`Pos(width, i)` is `Path::getPos`: node + int(width+1)/2, +0.5 y for flyable
nodes; `Index`, `Next`, `Done`, `Complete`, `SetSize`, `SameAs`).

## What is ported, from where

| Go | BDS (dump file) |
|---|---|
| `Finder.FindPath` start/goal selection for walkers, swimmers, fliers | `PathFinder::_findPath(ActorPathingData const&, float, float, float, float)` (PathFinder.cpp) |
| `search`: A*, h = 1.5 x distance, g += step + cost malus + jump cost, 140 expansions, partial path to the node with the smallest h | `_findPath(..., PathfinderNode*, PathfinderNode*, ...)`, `_reconstructPath` |
| neighbours: 4 walker directions (N, E, S, W) + horizontal diagonals without corner cutting, 6 directions + 12 diagonals for swimmers/fliers, climb-up neighbour, 32 neighbour cap, range pruning | `_getNeighbors`, `_addNeighborsWithDiagonals<4,0>/<6,0>/<6,1>`, `_isNeighborPotentiallyValid` |
| node creation: jump up to `max(jump, 1.2)` by collision tops, drop down to `max_fall_blocks` (default 3), jump cost | `_getNode` (both overloads), `_getHighestReachablePosByJumping`, `_getUnderneathSupportedNode` |
| footprint classification over floor(w+1) x floor(h+1) x floor(w+1) cells | `isFree`, `_classifyNode`, `_classifyDoorNode`, `_getPartialBlockNodeType`, `_getMinAndMaxHeightAroundBlock`, `_checkBlockAndNeighborsProperty`, `_checkForDamagingBlock` |
| cost malus (`default_block_cost`, `preferred_path_blocks`, -1 for blocked/lava) | `_getPathfindingMalus` |
| start node repair | `_findNearestPathableNeighbor`, `_isFreeStartNode`, `_getStartPositionFlyingActor` |
| swimmer nodes | `_getWaterNode`, `_isFreeWaterNode` |
| binary heap | BinaryHeap.cpp |
| block pathability: doors (blocked axis), trapdoors, bottom slabs, fire, campfire, liquids, sweet berry bush, scaffolding | `Block::checkIsPathable` and the overrides in DoorBlock/TrapDoorBlock/SlabBlock/FireBlock/CampfireBlock/LiquidBlock/SweetBerryBushBlock/ScaffoldingBlock.cpp |
| climbable test (powder snow, scaffolding) | `BlockClimberSystem::canBlockBeAscendedByJumping` |
| `Navigation.Tick` for walk: node skipping within max(w/2, 0.6), same-y look-ahead, door blocking, swim/fly shortcut, waypoint height correction, stuck check (100 ticks, 1.5 blocks) | `PathNavigation::tick`, `updatePath`, `_getHighestBlockHeight`, `_isPositionOnlyInAir`, `getTempMobPos`, `NavigationUtility::getSurfaceY`, `isDoorBlockingPath`, `canMoveDirectly`, `canWalkDirectly`, `canWalkOn`, `moveCondition`, `getMobScale` |
| `moveTo` (same path kept, avoid_sun trim), `canUpdatePath` per kind | `PathNavigation::moveTo`, `NavigationUtility::trimPathFromSun`, `*PathNavigation::canUpdatePath` |
| climb: climbing flag on horizontal collision, walk straight at the target after the path | `WallClimberPathNavigation::tick/createPath/moveTo` |
| fly/hover tick | `FlyingPathNavigation::tick/updatePath`, `HoverPathNavigation` |
| generic in water | `GenericPathNavigation::tick` (simplified, see gaps) |
| JSON options and defaults | `NavigationDescription::deserializeData`, constructor; `NavigationComponent::initializeFromDefinition` (field mapping) |
| walker/swimmer/flier per kind | `*PathNavigation::initializeInternal` |
| preferred path | `PreferredPathDescription::deserializeData`, `PathFinder::DEFAULT_DESC` |

Read from the 1.21.3 binary (`bds-1.21.3-pdb/bedrock_server.exe`, .rdata): the diagonal pair table
(`unk_142E9EA50`) and both direction orders (`qword_142E9EB68`, `unk_142E9EB70`). The 18-entry
offset table itself is filled at run time; it is rebuilt from Facing order and the pair table.
Field names of `ActorPathingData`, `PathfinderNode`, `NavigationComponent` and the PathFinder flag
bytes were recovered from the constructors and `extractPathingData`; enums (`NodeType`,
`BlockProperty`, `MaterialType`) come from the LeviLamina headers staged with the dump.

BDS quirks kept on purpose: a node's cost malus grows each time it is reached; flier nodes get the
malus twice; the follow-range check is measured from the goal; nodes are typed by the last
classification; `trimPathFromSun` cuts one node before the first sunlit one and only when the mob is
in shade; a search that starts in water with `can_float` ignores `avoid_water`.

## Dragonfly mapping (blocks.go)

BDS reads one collision AABB per block; here it is the union of the Dragonfly model boxes (fences
and walls are 1.5 high, as in Bedrock). Material and property bits are derived from Dragonfly block
and model types: stairs, slabs, fence gates, fences/walls, carpets, doors, trapdoors, cactus, magma,
campfire, fire, lava, water, portals, chests, dried kelp, cauldron, ladders; by name for blocks
Dragonfly lacks (sweet berry bush, honey block, powder snow, scaffolding, iron trapdoor). "Solid" /
"blocks motion" is approximated by "has collision, not a ladder". Per-state facts are cached for the
process (keyed by `world.BlockHash`); shapes of neighbour-dependent models (fences, panes, bamboo,
models outside `block/model`) are computed per position.

## Performance

- Every search is fresh and synchronous, as in BDS. Finders (maps, node arena) come from a
  `sync.Pool`, so a mob keeps no search memory.
- Per-search caches: block info per position, nodes, floor/ceiling heights.
- Per-world `Budget` (default 8 searches or 1500 expansions per tick, `WorldBudget(w)`, used
  automatically when the source is a `*world.Tx`; set `Navigation.Budget`/`NoBudget` to override).
  A `MoveTo` over budget is queued, `Pending()` is true and `Done()` false until a later `Tick` runs
  it. Under the budget nothing changes from BDS behaviour.
- Benchmarks (`go test -bench . ./server/mob/nav/`, EPYC 9354P): a worst-case search (140
  expansions, 894 blocks read) takes about 0.34 ms; a 13-node path around a wall about 0.15 ms.

## Tests

`nav_test.go` builds structures in a synchronous world without generator: flat line (exact nodes),
diagonal, path around a wall, 1-block step (exact nodes), 2-block step refused, staircase, slab
staircase (exact nodes), fence line, fence gate open/closed, wooden door closed/open/can_open_doors,
iron door needing can_open_iron_doors, 3-block drop allowed, 4-block drop refused, max_fall_blocks
override, walking over water and avoid_water detour, lava detour, cactus detour, magma vs fire
immunity, blocks_to_avoid, the 140-expansion cap, follow range, spider footprint vs a 1-wide gap,
flier over a wall, swimmer over an underwater wall, and with a crude mover: following a path,
the stuck check after 100 ticks, a spider climbing a 4-high wall, budget deferral, avoid_sun trim,
option parsing. The pinned node lists are this port's output, checked by hand against the BDS rules;
they are not captured from a running BDS.

## Gaps

- `navigation.swim` (WaterBoundPathNavigation) and `navigation.generic` in water use a simplified
  tick (node skipping and direct swimming only; no WaterBound steering parameters). `navigation.float`
  paths like a walker; its `travel()` override belongs to the physics package.
- Riding (`canUpdatePath` for riders, vehicle on ground), `isPlayingDead`, Education border blocks,
  `canUseFreeStartNodeCheck` (two special entity types) and `FenceBlock::fetchPathableNeighbors`
  (approximated by the horizontal neighbours) are not ported.
- Fliers use the mob width as node reach radius (FlyingPathNavigation +12 default not recovered).
- Unknown BDS details approximated: the damage-immunity hook of the damaging-block check
  (fire immunity), the daytime test before trimming from the sun, the water visual height.
- Block tags in descriptors never match (Dragonfly has no block tags); copper doors count as metal.
