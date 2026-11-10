// Package jbrain is Java Edition's mob Brain system (net.minecraft.world.entity.ai.Brain
// and friends, 26.3 with Paper's default config) for the Java mob mode
// (mobs=java). Villagers, piglins, hoglins, wardens, allays, frogs, goats,
// axolotls, camels, sniffers, armadillos and copper golems run on it in Java.
//
// The package is generic over the body type E (the mob type a brain belongs
// to, usually a pointer to the mob wrapper opened in the current
// transaction) and knows nothing about the mob base: it only needs
// github.com/df-mc/dragonfly/server/world for entity handles and positions.
//
// # Pieces (Java class -> Go)
//
//   - MemoryModuleType<T> -> Memory[T], registered with RegisterMemory /
//     RegisterSavedMemory from init. Every vanilla memory exists as MemXxx
//     (memories.go). Entity references are *world.EntityHandle.
//   - Brain memory access -> Get, GetOr, Set, SetWithExpiry, SetOpt, Is on
//     *Memories; Memories.Has/Check/Erase/TimeUntilExpiry.
//   - MemoryStatus / entry conditions -> Req built with Present, Absent, Reg.
//   - Activity -> Activity (ActCore, ActIdle, ...; RegisterActivity).
//   - Behavior<E> -> NewBehavior(name, entry, min, max, task) with a Task
//     (embed TaskBase[E] and override CanStart/Start/CanStillUse/Tick/Stop;
//     implement TimedOut(now, end) to replace timedOut).
//   - OneShot / BehaviorBuilder.create -> Trigger(name, reqs, fn);
//     BehaviorBuilder.sequence/triggerIf -> Sequence, TriggerIf.
//   - GateBehavior / RunOne -> NewGate, RunOne with W(control, weight).
//   - Sensor / SensorType -> Sensor[E] and SensorType[E]{Name, Key,
//     ScanRate, New}.
//   - ActivityData -> ActivityData[E]; Seq(first, ...) is createPriorityPairs;
//     ActivityErasing is the create(activity, prio, list, memory) overload.
//   - Brain.Provider / makeBrain -> Provider[E].MakeBrain(e, rand, saved).
//   - Brain.tick -> Brain.Tick(Ctx{Tx, Time, Rand}, e).
//   - Brain.pack / Packed codec -> Brain.Pack().NBT() and Unpack(compound).
//
// # Tick contract
//
// A mob calls Brain.Tick once per server tick from its AI step (Java ticks
// the brain in Mob.customServerAiStep, i.e. the mob's own serverAiStep), with
// Ctx.Time = level game time and Ctx.Rand = the level random. Activity
// switching (setActiveActivityToFirstValid / updateActivityFromSchedule) is
// the mob's job after the brain tick, as in each Java XxxAi.updateActivity.
//
// Paper: tick-rates.sensor and tick-rates.behavior overrides are supported
// (SetPaperSensorRate / SetPaperBehaviorRate); Paper's default
// tick-rates.sensor.villager.secondarypoisensor = 40 is preset.
//
// Iteration order matches Java: priorities ascending; within a priority,
// activities in java.util.HashMap order of their name hashes; behaviours in
// insertion order; sensors in provider order.
package jbrain
