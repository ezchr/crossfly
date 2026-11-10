# server/mob/javamob: Java mob mode

`[Mobs] Behaviour = "java"` in config.toml (or `DF_MOBS=java`) runs the mobs as a
Paper 26.3 server does (vanilla Java Edition with Paper's default config), so Java
mob farms and mob behaviour work as players know them from Java servers. It is a
Crossfly Full feature (`crossfly_full` build tag). With `bedrock` or `off` nothing
of this package runs: the Bedrock mob engine (`server/mob`, behaviour-pack JSON) is
untouched, and a Java type costs the engine one nil check per tick.

Mobs that have no Java port yet keep their Bedrock-engine type in Java mode.

## How it fits together

* A Java mob is a `*mob.Mob` (the entity both clients see: metadata, health,
  effects, equipment, NBT, spawn eggs, /summon) whose type is registered with
  `mob.RegisterTypeWithHooks`: its definition has one component, the Java state
  (`state.go`), and the type's tick hook replaces the Bedrock stages with Java's
  `Mob.tick` (`tick.go`).
* `*javamob.Mob` embeds `*mob.Mob` and adds the Java side. Like `*mob.Mob` it is
  only valid inside its transaction; keep `m.H()` across ticks.
* Clients: Bedrock and Java clients render a Java mob from the same `mob.Meta`
  (`m.UpdateMeta`, `SetAggressive`, `StartUsingItem`, ...), exactly like a
  Bedrock-engine mob. Use the Meta field for anything a client shows.

| File | Java source |
|---|---|
| `registry.go` | `EntityType` (dimensions, category), `SpawnPlacements` hook, `MobCategory`, `EntitySpawnReason` |
| `attributes.go` | `Attributes`, `AttributeInstance`, `AttributeModifier` |
| `tick.go` | `Mob.tick`, `LivingEntity.baseTick/aiStep`, `Mob.serverAiStep`, jumping, drowning, `burnUndead` |
| `physics.go` | `LivingEntity.travel` (air, water, lava), `Entity.move/collide` (step-up), fall damage, block friction/speed/jump factors, `applyEffectsFromBlocks`, fluid currents (`entity.ApplyFluidCurrents`, `EntityFluidInteraction`) |
| `push.go` | `LivingEntity.pushEntities` with Paper's `max-entity-collisions: 8`, cramming |
| `goal.go` | `Goal`, `GoalSelector`, `WrappedGoal` |
| `control.go` | `LookControl`, `MoveControl`, `JumpControl`, `BodyRotationControl` |
| `targeting.go` | `Sensing`, `TargetingConditions`, `getNearestPlayer/Entity`, line of sight |
| `combat.go` | hurt hooks, `knockback`, `doHurtTarget`, death loot, equipment drops, XP, `DifficultyInstance`, `populateDefaultEquipmentSlots` |
| `loot.go` | entity loot tables (embedded `loot_tables/entities`, 26.3) |
| `randompos.go` | `DefaultRandomPos`, `LandRandomPos`, `GoalUtils`, home restriction |
| `neutral.go` | `NeutralMob` (persistent anger) |
| `spawn.go` | `EntityType.spawn` + `Mob.finalizeSpawn` |
| `path/` | `PathFinder`, `WalkNodeEvaluator`, `PathType`, `Path`, `PathNavigation`, `GroundPathNavigation`, `WallClimberNavigation` (with Paper's patches) |
| `goal/` | common goals: Float, LookAtPlayer, RandomLookAround, (WaterAvoiding)RandomStroll, MeleeAttack, LeapAtTarget, AvoidEntity, RestrictSun, FleeSun, RangedBowAttack, BreakDoor, NearestAttackableTarget, HurtByTarget |
| `monster/` | zombie (baby, drowned conversion, door breaking), skeleton, creeper, spider, enderman |

## Adding a mob

1. Make a package for your family (`server/mob/javamob/animal`, `.../nether`, ...)
   and add one blank import of it to `vanilla/vanilla.go`.
2. Write the class as a Go type with the class's own fields, and register it
   from `init`:

```go
func init() {
	javamob.Register(javamob.Type{
		ID: "minecraft:cow", Category: javamob.CategoryCreature,
		Width: 0.9, Height: 1.4, EyeHeight: 1.3,
		Attributes: func() map[javamob.Attribute]float64 {
			a := javamob.LivingAttributes()
			a[javamob.AttrMaxHealth], a[javamob.AttrMovementSpeed] = 10, 0.2
			return a
		},
		Families: []string{"cow", "mob"},
		XPReward: 0,
		New:      func() javamob.Behaviour { return &Cow{} },
	})
}

type Cow struct{ age int }

// RegisterGoals is Cow.registerGoals.
func (c *Cow) RegisterGoals(m *javamob.Mob) {
	m.Goals().Add(0, goal.NewFloat(m))
	m.Goals().Add(5, goal.NewWaterAvoidingRandomStroll(1, 0.001))
	m.Goals().Add(6, goal.NewLookAtPlayer(6))
	m.Goals().Add(7, goal.NewRandomLookAround())
}
```

3. Override Java methods by implementing the optional interfaces of `mob.go`:
   `Ticker`/`PostTicker` (tick before/after super), `AiStepper`,
   `ServerAiStepper` (`customServerAiStep`), `SpawnFinalizer`
   (`finalizeSpawn`), `DamageFilter`/`HurtListener` (`hurtServer`),
   `DeathListener`, `DeathLooter` (`dropCustomDeathLoot`), `Persister`
   (save data), `Attacker` (`doHurtTarget`), `SunSensitive`, `WaterSensitive`,
   `FarAwayRemover`, `Interactor` (`mobInteract`), `TargetFilter`
   (`canAttack`/`setTarget`), `BabySetter`, `Climber`, `HeadLimiter`,
   `WalkTargetValuer` (`getWalkTargetValue`; monsters use
   `javamob.MonsterWalkTargetValue`).
4. Goals: implement `javamob.Goal` (`CanUse`, `Flags` via `javamob.GoalBase`) and
   any of `CanContinueToUse`, `IsInterruptable`, `Start`, `Stop`, `Tick`,
   `RequiresUpdateEveryTick`. Goals shared by several mobs go to `goal/`; a goal of
   one mob stays in its package. Reuse a class by embedding its Go type (the
   zombified piglin embeds `monster.Zombie` and replaces its hooks).
5. Navigation other than ground walking: `m.SetNavigation(path.NewNavigation(kind, m))`
   in RegisterGoals (`path.WallClimber` for spiders; water, flying and amphibious
   kinds are still to be added to `path/`). Maluses:
   `m.SetPathfindingMalus(path.Water, -1)`.
6. Loot: the type's `entities/<name>` table is rolled on death
   (`Type.LootTable`, `NoLootTable`). Predicates the default entity_properties test
   does not know can be added with `javamob.SetEntityPredicate` (wrap
   `DefaultEntityPredicate`).
7. Tests: `server/mob/internal/mobtest` worlds; call `mobtest.RegisterTypes()`
   then `javamob.Install()` once before `mobtest.New` (see `javamob_test.go`).

## Brain-driven mobs

Villagers, piglins, wardens and the like use Java's Brain (sensors, memories,
activities, behaviours) instead of goals. The framework lives in its own package
(`server/mob/jbrain`); a mob plugs it in with `m.SetBrain(b)`, and the base ticks
it where `Mob.customServerAiStep` would (`javamob.Brain`).

## Spawning

Natural spawning is in its own package. Its hooks in the base:
`javamob.SetPlacement(id, Placement{Type, Heightmap, Rule})` (SpawnPlacements),
`javamob.SetDespawnCheck(func(*Mob))` (`Mob.checkDespawn`, run before every tick),
`javamob.Spawn(tx, id, pos, reason, opts)` (`EntityType.spawn` + `finalizeSpawn`),
and on the mob `RemoveWhenFarAway`, `RequiresCustomPersistence`,
`PersistenceRequired`, `NoActionTime`, `CheckSpawnObstruction`, `WalkTargetValue`.

## Not done yet

* Paper's entity activation range (spigot.yml `entity-activation-range`: inactive
  mobs far from players tick their goals every few ticks only).
* Ridden travel (controlling passenger, `tickRidden`, `getRiddenInput`), flying,
  water-bound and amphibious navigation, `MoveThroughVillageGoal`, picking up loot,
  bubble columns, honey sliding, the `Entity.checkInsideBlocks` sweep along the move
  (the blocks at the end of the move are checked).
* Dragonfly has no Looting enchantment yet, so loot rolls with looting 0.
