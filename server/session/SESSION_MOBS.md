# What the Bedrock session sends for mobs

All of this is optional and found by type assertion, so `server/session` does not import `server/mob`.
A method can sit on the entity, or on the `Behaviour()` of an `*entity.Ent`.
Code: `mob_view.go`, `entity_metadata.go`, `world.go`. Tests: `mob_view_test.go`.

## Entity type

| Method | Sends |
|---|---|
| `EntityProperties() []world.EntityProperty` (`PropertiedEntityType`) | one `SyncActorProperty` per type at join, right after `AvailableActorIdentifiers` (types from the world's entity registry). Only types with `ClientSync` properties get one, as on BDS. The data is `{type: id, properties: [{name, type 0 int/1 float/2 bool/3 enum, min, max \| enum}]}`. |

`world.EntityProperty` (in `server/world/entity_property.go`) has `Name`, `Type`, `Min`/`Max`, `Enum`, `ClientSync` and `Default`.
Network order works like BDS: the synced properties sorted by name, and a property's index is its place in that list.
A type with more than 32 properties gets none, as BDS does.
The schema is cached per identifier the first time it is used.

## Entity

| Method | Sends |
|---|---|
| `ActorProperties() map[string]any` | property values in `AddActor` and `SetActorData`: ints, bools (0/1) and enums (index) go in the int list, floats in the float list. An enum value can be its name or its index. A missing or unconvertible value falls back to a constant `Default`; a Molang default is left out. Every synced value goes out with each `ViewEntityState`, so **call `ViewEntityState` after changing a property**. |
| `Rotation()` | yaw and pitch (BDS `rot`: the way MoveControl turns the mob) |
| `HeadYaw() float64` | head yaw in `AddActor`, `MoveActorAbsolute` (rotation z) and the teleport. Without it, the head yaw is the yaw. |
| `BodyYaw() float64` | `AddActor.BodyYaw` only. BDS sends no body yaw in movement; clients turn the body themselves. |
| `TargetEntity() *world.EntityHandle` or `Target() *world.EntityHandle` | `EntityDataKeyTarget` (0 if there is no target or the viewer cannot see it) |
| `MobColour() item.Colour` | `ColorIndex` (sheep wool, wolf collar) |
| `MobColour2() item.Colour` | `ColorTwoIndex` |
| `Sheared()`, `Saddled()`, `Tamed()`, `Sitting()`, `WallClimbing()`, `Shaking()`, `Chested()` `bool` | the flag of the same name (`WallClimbing` is the spider climb flag) |
| `Ignited() bool` | Ignited flag plus `FuseTime`. The creeper swell is worked out on the client from this flag (Creeper::normalTick), so no swell data is sent. |
| `FuseLength() time.Duration` | `FuseTime` while ignited (default 30 ticks) |
| `Powered() bool` | Powered flag (charged creeper) |
| `BedrockMetadata(protocol.EntityMetadata)` | runs last and may change any actor data |
| `Health()`, `MaxHealth()` (+ optional `Speed()`, `Absorption()`, `BedrockAttributes() []protocol.AttributeValue`) | `AddActor.Attributes` (health, movement, absorption, extras) for every entity that is not a player |
| `HeldItems()` (`item.Carrier`) | `MobEquipment` main and off hand. Invisible mobs still show what they hold; only hidden players do not. |
| `Armour() *inventory.Armour` and/or `BodyArmour() item.Stack` | `MobArmourEquipment`, with body armour in `Body` |

Flags and data that already existed still work: `Baby()`, `Scale()`, `Variant()`/`MarkVariant()` (int32), `Angry()`, `Invisible()`, `OnFireDuration()`, `Immobile()`, `NameTag()`, `Effects()`, `Breathing()`/air supply.

## Viewer calls a mob makes

These are on `world.Viewer` or the session:
- `ViewEntityState(e)` after changing actor data or properties: sends `SetActorData` with metadata and properties.
- `ViewEntityMovement(e, pos, rot, onGround)` each tick it moves or turns, including head-only turns: the world only sends movement when `Rotation` or the position changes.
- `ViewEntityItems(e)` and `ViewEntityArmour(e)` after equipment changes.
- `ViewEntityHealth(e)` after health changes. It is not on `world.Viewer`, so assert `v.(interface{ ViewEntityHealth(world.Entity) })`. It sends `UpdateAttributes` with the same list as `AddActor`, and does nothing for players.

## Not done

- `MoveActorDelta`: Dragonfly only sends `MoveActorAbsolute`, which clients take the same way.
- BDS sends only changed properties (`packDirtySyncData`); we send all synced values each time.
- `conditional_bandwidth_optimization`.
