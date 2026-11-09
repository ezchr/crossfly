# server/mob contracts (Phase 0)

What every mob package builds on. Package `mob` holds only these contracts;
behaviour lives in sub-packages that register themselves. Read this, then the
doc comments of the files named below; they are the spec.

Build/test: `go build ./... && go vet ./server/mob/... && go test ./server/mob/...`
(the shared VPS has 4 cores: use `-p 2`).

## Ownership

| WP | Package(s) | Owns |
|---|---|---|
| Phase 0 | `mob`, `mob/data`, `mob/vanilla` | Mob type, registries, goal selector, tick order, embedded JSON. Only the coordinator edits these after Phase 0 (one import line per package in `vanilla/vanilla.go`). |
| A1 | `mob/def`, `mob/event` | `Definition`/`Instance` (groups, diffable active set, events, properties, Molang subset, NBT of groups/properties). Installs `mob.SetDefinitionParser`. |
| A2 | `mob/filter` | Filter parser + the test-name registry + all 70 tests. Installs `mob.SetFilterCompiler`. |
| B | `mob/nav` | Pathfinder, `mob.Navigation`/`mob.Path`, components `minecraft:navigation.*`, `preferred_path`, annotations used by paths. |
| C | `mob/control`, `mob/physics` | Move/Look/Jump/Body controls, `movement.*`, `jump.*`, `SetTravel`, `SetKnockBack`, fall damage, push, `Physics`/`Input` contents. |
| D | `mob/goal/core`, `mob/component/core` | Passive goals; attribute components (`health`, `movement`, `underwater_movement`, `follow_range`, `attack`, `knockback_resistance`, ...), `type_family`, `collision_box`, `scale`, baby/ageable/breed, interact, loot/xp, variants, timer, ... |
| E | `mob/goal/combat`, `mob/component/combat` | Hostile goals and components (shooter, explode, burns_in_daylight, sensors, transformation, break_door). |
| F | `mob/spawn` | Spawn rules (`data.SpawnRules()`), caps, natural spawning, `despawn`/`persistent` (StageDespawn systems). |
| G1 | `server/session` | Bedrock client: properties, metadata, head/body yaw. Reads the Mob accessors below. |
| G2 | `server/java/javasession` | Java client translation of the same. |
| H | `mob/internal/mobtest`, `mob/debugcmd` | Headless world harness and scenario tests; `/mobdebug` lives in `server/mob/debugcmd` (servers register `debugcmd.Command`). |

Since the integration pass (2026-11-09) one owner holds every package under
`server/mob` plus the hooks below; the table records where each part came from.

## Registration pattern

Each package has a `register.go` with an `init()` that only calls `mob.Register*`
/ `mob.Set*`. The coordinator adds one blank import per package to
`server/mob/vanilla/vanilla.go`. Registering a key twice panics. Register only
from `init` (registries are read lock-free by every world goroutine).

```go
func init() {
	mob.RegisterGoal("minecraft:behavior.random_stroll", newRandomStroll)       // GoalFactory(spec GoalSpec) (Goal, error)
	mob.RegisterComponent("minecraft:health", newHealth)                        // ComponentFactory(key, raw) (Component, error)
	mob.RegisterSystem(mob.System{Name: "spawn/despawn", Stage: mob.StageDespawn, Tick: despawnTick})
	mob.RegisterInitHook(mob.InitHook{Name: "control/defaults", Init: func(m *mob.Mob) { m.SetLookControl(...); m.SetBodyControl(...) }})
	mob.SetTravel(travel)                 // C only
	mob.SetKnockBack(knockBack)           // C only
	mob.SetDefinitionParser(parse)        // A1 only
	mob.SetFilterCompiler(compile)        // A2 only
	mob.SetMovementSender(send)           // whoever implements conditional_bandwidth_optimization
	mob.RegisterHurtHook(mob.HurtHook{Name: "pkg/x", Hurt: f}) // every hit on every mob
	world.RegisterTickHook(f)             // once per world tick (spawn uses it)
}
```

Packages that defined their own small interfaces (`nav/iface.go`,
`def/iface.go`, `filter/iface.go`) adapt in `register.go`: `*mob.Mob` is meant
to satisfy those interfaces directly; the adapter only converts between the
package's types and `mob.Navigation`, `mob.Definition`/`mob.Instance`,
`mob.Filter`. Package `mob` never imports a sub-package (no cycles).
When Phase 0 landed, none of those iface.go files existed yet, so the Mob
methods follow the plan; if your interface needs a method `*mob.Mob` lacks,
name it in your report (or adapt in register.go), don't edit package `mob`.

## Lifecycle

- `mob.RegisterType(def)` makes a `world.EntityType` per identifier;
  `vanilla.Register(opts)` does it for the 92 vanilla mobs (skipping ids passed
  in `Skip`, e.g. Dragonfly's own types). Add `mob.Types()` to the entity registry.
- `mob.Spawn(tx, id, pos, SpawnOptions)` adds the mob, initialises it, then fires
  `minecraft:entity_spawned` (or `opts.Event`, e.g. `mob.EventBorn`).
  `mob.NewHandle` is for callers that add the handle themselves (fires on first tick).
- Init (Spawn, or first tick after loading): `Definition.NewInstance()`, saved
  instance NBT, init hooks, components attached in definition order, saved
  component NBT, spawn event. Loaded mobs do not fire spawn events.
- Component set changes: `m.Fire(event, ctx)` -> `Instance.Fire` returns
  `changed` -> the mob diffs `Instance.Components()` by key and raw JSON: gone or
  changed ones are detached (last first) / goals removed, new or changed ones
  built and attached in definition order. Unchanged components keep their state,
  except the keys the instance reports through the optional
  `InstanceReinit.TakeReinit()` (package def: `Diff.Reinit`, a group added
  again or overridden): those are rebuilt like BDS does (a re-added timer
  restarts). Optional `InstanceGroups.ActiveGroups()` lists the added groups.
- Goals are components whose key is registered with `RegisterGoal`; they go into
  `m.Goals()` with the JSON `priority` (default 0) via `GoalSelector.Add`.
- Unknown keys are recorded (`mob.UnsupportedSeen()`) and do nothing. Factory
  errors are logged once per entity/key (`mob.FactoryErrors()`).
- Death: health 0 -> goals stopped, nav/move stopped, `DeathAction`,
  `DeathListener`s; lies 20 ticks running only travel/despawn/send, then closed.

## Goal selector (selector.go; BDS GoalSelectorComponent/GoalSelectorUtility)

One list for both kinds, insertion order (`Add` replaces same key and appends
at the end, without Stop, like `addGoal`; `Remove` does not Stop either).
Per tick: tryStart(target), tick(target), tryStart(main), tick(main).

- `canUseInSystem(g)`: for every other used goal o: if `g.prio < o.prio`, o must
  be interruptible (any kind!); else if same kind and flags overlap -> blocked.
- tryStart: used goal keeps running iff canUseInSystem && CanContinueToUse, else
  Stop (used=false); then any goal (also just stopped) with canUseInSystem &&
  CanUse is marked used+toStart; if any was marked, every used goal (both kinds)
  failing canUseInSystem is stopped.
- tick: Start goals with toStart, then Tick every used goal, list order.
- Goals added/removed during a pass (event fired from a goal) apply after the
  pass (BDS moves the vector out during passes).
- `Goal` needs `CanUse(m)`, `Flags()`; optional `GoalContinuer` (default CanUse),
  `GoalInterruptible` (default true), `GoalStarter`, `GoalStopper`, `GoalTicker`,
  `TargetGoal` (default false), `GoalDebugger`. Embed `mob.GoalBase` for Flags and set
  `Control: spec.FlagsOr(<BDS default>)`. Flags: `FlagMove=1, FlagLook=2, FlagJump=4`.
- Lower priority number = more important. Tests: `selector_test.go`.

## Tick order (tick.go)

`Ent.Tick` (fire countdown, void) -> state.Tick: effects, then for each stage:
built-in step, then systems by (Order, Name), then component `Ticker`s of that
stage in definition order (`StagedTicker.TickStage()`, default StageSensors).

| Stage | Built-in step |
|---|---|
| `StageSensors` | - (timers, sensors, ageable, breathable...) |
| `StageGoals` | `m.Goals().Tick(m)` |
| `StageNavigation` | `m.Navigation().Tick(m)` |
| `StageControls` | MoveControl, JumpControl, LookControl `.Tick(m)` |
| `StageTravel` | travel hook (default: gravity 0.08, drag 0.02, block collision) |
| `StageBody` | `m.BodyControl().Tick(m)` |
| `StageEnvironment` | 1 fire damage/s while burning, block `EntityInside` (lava, fire, cactus, cobweb unless `IgnoresCobwebs`...), water extinguishes |
| `StageDespawn` | - (F) |
| `StageSend` | `m.SendMovement()` or the `SetMovementSender` hook |

A mob that dies stops the AI stages at once and keeps only travel/despawn/send.
`Behaviour.Tick` returns nil: movement is sent in StageSend.

## Mob API (mob.go, meta.go, living.go)

`*mob.Mob` embeds `*entity.Ent` (Tx, Position, Velocity, SetVelocity, fire,
name tag, Close...) and implements `entity.Living`. Valid only in its tx; keep
`m.H()`. `mob.Of(e)`, `mob.OfHandle(tx, h)`.

- Identity: `Identifier`, `Type`, `Definition`, `Instance`, `Rand` (per-mob PCG;
  use it for every roll), `SetRandSeed`, `Ticks`.
- Position/rotation (degrees): `Pos`, `Vel`, `SetPosition` (no viewer update),
  `EyePosition`, `EyeHeight` (0.85·h·scale), `BBox`; `Yaw`/`SetYaw` (facing,
  = `Rotation()[0]`), `Pitch`/`SetPitch`, `BodyYaw`/`SetBodyYaw`, `HeadYaw`/`SetHeadYaw`.
- Physics/input: `Physics()` (*Physics: OnGround, HorizontalCollision,
  VerticalCollision, InWater, InLava, Underwater, FallDistance), `OnGround`,
  `Input()` (*Input: Forward, Strafe, Vertical, Speed, Jump).
- Attributes: `Attributes()` (`Get/Value/SetBase/SetRange`), `Attr(name)`;
  names `mob.AttrMovement`, `AttrUnderwaterMovement`, `AttrLavaMovement`,
  `AttrFlyingSpeed`, `AttrFollowRange`, `AttrAttackDamage`,
  `AttrKnockbackResistance`, ... (BDS defaults until set). Health is
  `Health/MaxHealth/SetMaxHealth/SetHealth`; `Speed()` = movement attribute.
- Families: `Families`, `HasFamily`, `SetFamilies` (type_family component sets it).
- Combat memory: `Target`/`TargetHandle`/`SetTarget`/`ResetTarget` (notifies
  `TargetListener`s), `LastHurtBy() (e, tick, ok)`, `SetLastHurtBy`,
  `LastHurtByPlayerTick`, `LastHurtMob`/`SetLastHurtMob`.
- Controls: `Navigation`, `MoveControl`, `LookControl`, `JumpControl`,
  `BodyControl` (+ `Set*`, nil = no-op), `StopMovement`. Interfaces in control.go.
- Goals: `Goals()` (*GoalSelector: Add/Remove/Goal/Priority/Running/
  RunningGoals/Keys/StopAll/StopNonTargeted/Clear*/DebugInfo).
- `HasNavigation`, `FallDistance`/`ResetFallDistance` (blocks reset it),
  `InactivityTicks`/`ResetInactivity` (Mob::mNoActionTime: counts every
  alive tick, reset by Hurt; despawn and the inactivity_timer filter read it),
  `SetIgnoresCobwebs` (block_movement_slowdown_immunity), `PlaySound(event)`
  (`mob.Sound`: Bedrock LevelSoundEvent with the entity type, Java
  entity.<type>.<event>; Hurt plays "hurt", death "death"), riding stubs
  `Vehicle`/`Passengers`/`Riding` (never riding yet).
- Components: `HasComponent(key)` (active, supported or not), `ComponentJSON`,
  `Component(key)`, `Components()`, generic `mob.ComponentOf[T](m)`.
- Definition: `Fire(event, ctx)` (built-ins `mob.EventSpawned`, `EventBorn`,
  `EventTransformed`, `EventGrowUp`, `EventOnTame`, ...), `ReapplyComponents`,
  `Property`/`SetProperty` (refreshes viewers)/`Properties`.
- Misc: `Home/SetHome/ClearHome`, `Persistent/SetPersistent`,
  `Data(key)/SetData(key, v)` (per-mob scratch for any package; use an
  unexported key type), `HeldItems/SetHeldItems`, `Armour`, `Interact`,
  `Hurt`, `Heal`, `Kill`, `KnockBack`, effects.
- Client state: `Meta()` / `UpdateMeta(func(*Meta))` (refreshes viewers on change):
  Width/Height (collision_box), Scale, Baby, Variant, MarkVariant, SkinID, Color,
  Color2 (dye ids), Sheared, Saddled, Tamed, Sitting, Chested, Charged, Ignited,
  FuseTicks, Swell, Climbing, Shaking, Converting, InLove, Angry, Aggressive, Sneaking,
  UsingItem, TradeTier, Invisible, NoGravity, NoAI, Owner; plus `SetBodyArmour`,
  `SetHeldItems`, `Armour()`. The getters match what G1 (`session/mob_view.go`)
  and G2 (`javasession/mob_iface.go`) probe, on both `*Mob` and its behaviour
  (session probes the behaviour too, and `entity.Ent` refreshes state with
  itself): `Baby, Variant, MarkVariant, Colour/Colour2 (int32), MobColour/
  MobColour2 (item.Colour), Sheared, Saddled, Tamed, Sitting, Chested, Charged,
  Powered, Ignited, FuseLength, Climbing, WallClimbing, Shaking, Converting,
  Angry, Aggressive, Sneaking, UsingItem, TradeTier, Invisible, Gravityless,
  Immobile, Owner, HeadYaw, BodyYaw, TargetEntity, BodyArmour, Absorption,
  ActorProperties, Health/MaxHealth, Speed, Scale, HeldItems`. `*mob.Type`
  implements `EntityProperties() []world.EntityProperty`. `Type.BBox` =
  collision box x scale (default 0.6x1.8). Name mapping where the two
  clients differ (both names exist): session `WallClimbing` = java `Climbing`
  = Meta.Climbing; session `MobColour/MobColour2` (item.Colour) = java
  `Colour/Colour2` (int32 dye id) = Meta.Color/Color2; session `Powered` = java
  `Charged` = Meta.Charged; session `FuseLength` = Meta.FuseTicks (1.5 s when 0);
  session `TargetEntity` = the target handle. Health changes (Hurt, Heal,
  SetHealth, SetMaxHealth, Kill) call `ViewEntityHealth(m)` on viewers that have
  it. Actions you send with `ViewEntityAction` that are Bedrock actor events
  (eat grass, ...) must implement `ActorEvent() uint8` (gophertunnel
  `packet.ActorEvent*`) for both clients. `BedrockMetadata(m)` /
  `BedrockAttributes()` are not implemented by the core; a package needing raw
  actor data should ask for a hook. Add a new client field? Ask the
  coordinator for a Meta field rather than keeping it elsewhere.

## Component hooks (component.go)

`Component{Attach(m); Detach(m)}` plus optional: `Ticker`/`StagedTicker`,
`DamageModifier` (see/cancel/scale `*Damage` before it applies), `HurtListener`,
`DeathListener`, `Interactor` (first true wins), `TargetListener`, `Persister`
(`EncodeNBT()` runs outside any tx: own fields only; `DecodeNBT(m, data)` after
attach on load). Hurt order: modifiers -> fire resistance/resistance -> 10-tick
immunity (only damage above the last hit) -> health, LastHurtBy, hurt anim ->
HurtListeners -> HurtHooks -> death. Explosions (`entity.Ent.Explode`) hurt mobs
with `entity.ExplosionDamageSource` (Dragonfly's player formula) and push them.

## Filters (filter.go)

`mob.CompileFilter(raw)` once at factory time -> `Filter.Test(*FilterContext)`.
Empty/absent filters pass; before A2 is linked every non-empty filter is false.
`FilterContext{Tx, Self, Other, Target, Player, Parent, Baby, Damager, Block,
BlockPos, HasBlock, Item, Damage}`; `m.Ctx()` gives Self+Tx.

## Definitions (definition.go)

`Definition{Identifier, Description, ComponentKeys, NewInstance}`,
`Instance{Components, Fire, Property, SetProperty, Properties, EncodeNBT,
DecodeNBT}`, `Description` (incl. `[]PropertyDecl` for client sync),
`mob.ParseOrderedObject` (JSON object in key order). Without A1, a fallback
reader gives description + base components only. A1 must keep
`Components()` order stable and only expose group changes after `Fire` returns.

## Data

`data.Entities()` (129 files, 1.26.63 merged), `data.SpawnRules()` (60),
`data.Entity("zombie.json")`. `vanilla.Definitions()`, `vanilla.IsMob(def)`.
`go test -v ./server/mob/vanilla -run TestLoadAll` prints the unsupported-key
report (set `MOB_SUPPORT_REPORT=file` to save it); `TestSpawnAllMobs` spawns and
ticks every mob with whatever is linked and fails on factory errors.

## Hooks outside server/mob

- `server/entity/mob_hooks.go`: `entity.BlockBBoxesAround(tx, box)` (the block
  boxes `MovementComputer.CheckCollision` uses), for mob physics.
- `server/world/tick_hook.go`: `world.RegisterTickHook(func(tx))`, run once per
  world tick after the entities (only while the world ticks, i.e. has viewers or
  is synchronous). Natural spawning runs from it (no ticker of its own).
- `server/session` / `server/java/javasession`: `InLove()` (Bedrock flag
  in_love; Java gets the love-hearts entity event once) and sounds with
  `LevelSoundEvent()` (`mob.Sound`).

## Integration notes (2026-11-09)

- `vanilla/vanilla.go` links every Phase 1 package; `nav/register.go` adapts
  package nav (`navigation.*`, `preferred_path`, `nav.MobNavigation` with
  `SetAvoidWater`, run-time traits: step height, follow range, fire immunity,
  open/break door by difficulty).
- Follow range: the vanilla Phase 1 files set no `minecraft:follow_range`, so
  it is BDS's default 16 (`Mob::_registerMobAttributes`). It bounds target
  goals (`TargetGoal::_withinRange`) and path searches (nodes within 16 of the
  goal), as in BDS.
- Attribute modifiers (baby speed, effects) are still not modelled:
  `Attribute.Value` is the base unless a package sets it.
- BDS `removeGoal`/`addGoal` do not call Stop on the old goal; we match that.
