# server/mob/def, server/mob/event, server/mob/filter

Data-driven entity definitions: every vanilla mob comes from Mojang's own
entity JSON. Semantics follow the BDS 1.21.3 dump (`ActorDefinition*`,
`ActorDefinitionEvent*`, `ActorDefinitionDiffList`, `ActorPropertyUtils`,
`FilterGroup`, `ActorFilterGroup`, `FilterTest*`, `Actor*Test`); newer pieces
(first_valid, 1.21.50+ tests) follow the Bedrock docs and are marked as such.

## Linking

`def/register.go` installs `mob.SetDefinitionParser(def.ParseMob)` and
`filter/register.go` installs `mob.SetFilterCompiler(filter.Compile)`. A
server only needs the blank imports in `server/mob/vanilla/vanilla.go`:

```go
_ "github.com/df-mc/dragonfly/server/mob/def"
_ "github.com/df-mc/dragonfly/server/mob/filter"
```

(`event` is imported by `def`.) Component packages keep using
`mob.RegisterComponent`/`mob.RegisterGoal` with the raw JSON; this package
does no typed decoding.

## def

```go
d, err := def.Parse(fileBytes)          // partial errors leave d usable
set, err := def.LoadFS(fsys, "entities") // *.json, recursive; Set.Get(id)
inst := d.NewInstance()                 // description entry + base components
inst.AddGroup("minecraft:cow_adult")     // push "+group"; false for unknown groups
inst.RemoveGroup("minecraft:cow_baby")   // push "-group"
inst.Components() / Component(name) / HasComponent(name) // live active set, stable order
diff := inst.CommitDiff()                // Added / Removed / Reinit since last commit
inst.PropertyValue(name)                 // int, float64, bool, string
inst.SetProperty(name, molang.Value)     // queued until Commit (BDS tryQueue*)
inst.SetPropertyNow(name, v)             // immediate, type/range checked
inst.Save() / inst.Load(m)               // NBT: "definitions" + "properties"
def.SplitEvent("minecraft:drowned<minecraft:as_adult>") // id, spawn event
```

**Group stack** (exactly `ActorDefinitionDiffList`): entry 0 is the
description (`+minecraft:cow` in NBT), entry 1 the base components (`+`), then
one entry per add/remove in order. The active set is the fold: an add
overrides components by name, a remove deletes every component name its group
declares *wherever it came from* (base components included). After each push
the stack is compacted like `_updateStack` (entries fully overridden by later
ones are dropped; removals that overlap nothing below are dropped), which never
changes the result. `HasGroup` (`query.had_component_group`) is true when the
group's newest entry is an add. Groups an event names that the definition does
not have (zombie `minecraft:on_foot`, silverfish's malformed `remove`) are
ignored, as in BDS.

**Diff**: `Reinit` lists components still active but now supplied by a newer
stack entry (a group re-added, or overridden by another group). BDS rebuilds
those. Note: `mob.applyComponents` (Phase 0) diffs by key + raw JSON, so a
re-added group with identical JSON keeps the old component state (a timer is
not restarted). Use `Diff.Reinit` if that needs to match BDS.

**Properties**: int/float (`range` required, default clamped), bool, enum
(2..16 values, `^[a-zA-Z][a-zA-Z0-9_]*$`, ≤ 32 chars), 32 per entity, file
order (`Index`). Defaults may be Molang (bee `query.had_component_group`),
evaluated at `NewInstance` and again after `Load` for unsaved properties.
Setting an out-of-range number or an unknown enum value is an error and leaves
the value, like BDS. Floats are kept at float32 precision.

**NBT** (under `mob:definition` in the mob's compound):
`definitions: ["+minecraft:cow", "+", "+minecraft:cow_adult", "-minecraft:cow_baby"]`,
`properties: {name: TAG_Int | TAG_Float | TAG_Byte | TAG_String}`. Unknown
groups/properties are skipped with an error.

**mob adapters** (`register.go`): `MobDefinition` (`mob.Definition`),
`MobInstance` (`mob.Instance`; `Fire` builds the filter context from
`mob.FilterContext`, runs the event, refreshes viewers after property changes
and calls `ReapplyComponents` on other mobs reached by triggers),
`MobEntity` (the event entity: handles `reset_target`, `stop_movement`,
`set_home_position`, `drop_item` on the mob; `play_sound`,
`emit_vibration`, `emit_particle`, `execute_event_on_home_block` are no-op
hooks; `queue_command` goes to `Instance.Commands`, a 64-entry log).
`mobQueries` answers the Molang queries the vanilla files use
(`is_baby`, `variant`, `mark_variant`, `skin_id`, `is_on_ground`,
`head_is_in_water`, `is_using_item`, `last_hit_by_player`,
`equipment_count`, `has_any_family`, health, ...).

## def/molang

`molang.Parse(src)` → `*Expr`, `Eval(env)`; `Env.Query(name, args)` resolves
`query.*`/`q.*`. Supported: numbers, `'strings'`, true/false, `! -`,
`* / + - < <= > >= == != && || ??`, `a ? b : c`, `a ? b`, parentheses,
`;` statements with `return`, `variable./temp./context.` reads and
assignment, `this`, and `math.` abs, acos, asin, atan, atan2, ceil, clamp,
cos, sin, exp, ln, floor, round, trunc, sqrt, pow, max, min, mod, sign, lerp,
inverse_lerp, lerprotate, min_angle, hermite_blend, random, random_integer,
die_roll, die_roll_integer, pi. Case-insensitive (`Math.Random`). Unknown
queries read 0.
Not supported (unused by the 1.26.63 behaviour pack): arrays, `->` member
access, `loop`/`for_each`/`break`/`continue`, `{}` blocks, geometry/texture/
material references.

## event

```go
r, err := event.Parse(raw)                      // one event
event.Fire(entity, "minecraft:entity_spawned", ctx)
```

`Entity` = `filter.Actor` + `EventState() event.State` (`*def.Instance`
implements State). Execution is `ActorDefinitionDescriptor::_executeEvent`:

1. loop check on (event, name tag) over the whole chain (keys are never
   removed, so one chain runs each pair once; BDS behaviour);
2. root `filters`; then the tree is *collected* (`_evaluateGroups`):
   sequence evaluates all its children's filters first, randomize draws only
   among children whose filters pass (weights as float32), first_valid takes
   the first passing child. Nothing runs before the whole event is collected,
   so all filters of one event see the state from before it;
3. per collected leaf in order: remove groups, add groups, actions;
4. `State.Commit()` (queued properties, component diff);
5. triggers whose filters pass now, on self / other / target (the entity's
   target) / baby (self = baby, other = parent) / block. Player, parent and
   damager trigger targets do nothing (BDS 1.21.3).

A node with randomize ignores sequence and any add/remove/trigger next to it;
sequence ignores add/remove next to it (villager_v2 `spawn_from_village` has
such a dead `add`). Empty collections are invalid and dropped.

Actions: `SetProperty`, `QueueCommand`, `PlaySound`, `EmitVibration`,
`EmitParticle`, `ResetTarget`, `StopMovement`, `SetHomePosition`,
`DropItem`, `ExecuteEventOnHomeBlock`, `Unknown` (kept, never fatal). An
entity implementing `event.ActionHandler` sees them first, then
`State.HandleAction`. Engine-fired event names are constants in
`builtin.go` (`event.EntitySpawned`, `EntityBorn`, `EntityTransformed`, ...).
`event.ParseTrigger` parses the `{"event","target","filters"}` objects that
sensor/timer/interact components use.

## filter

```go
f, err := filter.Parse(raw)  // or ParseValue(decoded), MustParse
ok := f.Eval(&filter.Context{Self: a, Other: b, World: w, Rand: r})
```

`Filter{Kind, Groups, Tests}` mirrors `FilterGroup`. Parsing: arrays merge
into the current group; `{"test": ...}` is one test; members `all_of`/`all`/
`AND`, `any_of`/`any`/`OR`, `none_of`/`NOT` open subgroups, except that a
member of the same kind as the current group merges into it (BDS: so
`none_of` inside `none_of` flattens); other members are legacy forms
(`other_with_families`, `with_components`, `is_world_difficulty`, `day`,
`brightness_less`, ...; table from `ActorFilterGroup::initialize`).
`subject`/`operator` take names or the BDS numbers (subject 0 self, 1 other,
2 player, 3 target, 4 parent, 6 block, 7 damager; operator 0 ==, 1 !=, 2 >,
3 <, 4 >=, 5 <=). Evaluation: all_of fails on the first failure, any_of passes
on the first pass (empty any_of is false), none_of fails on the first pass. A
test whose subject is missing fails before its operator applies. Unknown tests
log once (`filter.Unknown()`) and evaluate false.

Subject interface (`iface.go`): `Actor{Position, HasFamily, HasComponent,
Flag(Flag), Number(Number)}`, plus optional `Related`, `Equipped`,
`SlotChecker`, `PropertyHolder`, `Effects`, `Abilities`, `Tagged`,
`Coloured`, `Trusting`, `InBlock`; `World` (embed `NopWorld`) and `Block`
for environment and block subjects. A missing capability reads as the zero
value. 105 tests are registered (`filter.Registered()`), covering all 84 test
names the vanilla files use; `filter.Register` adds custom ones.

`adapt.go` maps Dragonfly to these interfaces: `filter.FromMob(*mob.FilterContext)`,
`filter.Wrap(tx, entity)` (`EntityActor`, identity = entity handle),
`TxWorld`, `TxBlock`, `ItemOf`, `DamageCause`. For a mob, `has_component`,
`type_family` and variants read the live definition instance, so filters
inside an event chain see the groups that event already changed.

Things the core mob does not model yet, answered by defaults until a package
registers a `filter.RegisterFlagSource` / `RegisterNumberSource`:
riding (`is_riding`, `rider_count` = 0, `is_vehicle_family`,
`is_riding_self`, controlling passenger), leashes, caravans, trust,
`has_container_open`, `in_caravan`, `inactivity_timer` (spawn/despawn
package), `is_sitting`/`is_tamed` beyond `Meta`. Item tags are derived from
the item name (Dragonfly has no item tag registry). `is_game_rule` uses
`filter.GameRules` (Bedrock defaults). `is_raider` approximates with the
illager/witch/ravager families. `actor_has_item_with_enchantment_in_slot`
(newer than the dump) maps numeric domain 7, its only vanilla use, to feet.

## Tests

`go test ./server/mob/def/... ./server/mob/event/... ./server/mob/filter/...`

- all 129 vanilla definitions (`def/testdata/entities`, 1.26.63 merged) parse
  with no errors and no unknown event responses;
- all 1011 vanilla filters parse with no unknown test;
- stack/compaction/diff, properties, NBT round trip on a synthetic definition;
- Phase 1 scenarios on the real files: cow/pig/chicken climate variants from
  biome tags and the 95/5 baby split, cow growing up, chicken eggs by climate
  and rider count, sheep shearing and wool regrowth, zombie drowned
  conversion (start, stop, timer, adult/baby result), skeleton bow/melee
  switching by difficulty and lightning immunity (legacy filter), spider
  jockey odds, creeper ignition and charged explosion, every event of the
  eight mobs runs;
- `TestMobIntegration` (package def_test) spawns real `*mob.Mob`s in a
  world with both parsers linked: desert cow is warm, filters through
  `mob.CompileFilter`, zombie conversion through `mob.Fire`, NBT round trip,
  and every vanilla mob spawns and ticks.
