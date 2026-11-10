//go:build crossfly_full

// Package hostile holds the mobs of the "other hostiles & water" row of the
// Java mob mode (mobs = "java"): they behave as on a Paper 26.3 server with
// Paper's default configuration, so farms built for Java work the same.
//
// Every type here is a port of the vanilla 26.3 class named in its comment
// (net.minecraft.world.entity...), with Paper's changes where Paper's default
// configuration changes behaviour; those are marked "Paper:". The mobs are
// *mob.Mob entities run by the Java mob base (package javamob): this package
// only adds the per-type behaviour (goals, controls, ticks, hurt and death
// hooks) and the client state both editions render (mob.Meta, actor flags,
// actor properties).
//
// The default Bedrock mob engine never reaches this package: nothing here
// runs unless the Java mob mode registered the types.
//
// # What is here
//
//   - cube.go: AbstractCubeMob (sizes 1..127, size-scaled box, health size²,
//     speed 0.2+0.1·size, spawn sizes, split on death, CubeMobMoveControl,
//     float/random-direction/keep-jumping/attack goals, contact damage) as
//     CubeKind; slime is one kind, the nether row's magma cube another.
//     Paper: the attack goal ends on a dead target and clears the target.
//   - guardian.go: Guardian and ElderGuardian: attack goal (10-tick stare,
//     80/60-tick beam, 1 magic +2 hard +2 elder, then a melee hit), target
//     selector (players, squid, axolotls further than 3 blocks), thorns
//     while the spikes are out, flopping, GuardianMoveControl, travelInWater,
//     the elder's Mining Fatigue III curse every 1200 ticks within 50 blocks.
//   - fish.go: AbstractFish (air out of water, flop, FishMoveControl,
//     travelInWater, bucket flag), AbstractSchoolingFish (schools,
//     FollowFlockLeaderGoal), salmon sizes, pufferfish puffing and stings,
//     tropical fish variants (the 22 common ones, packed ids).
//   - phantom.go, phantom_spawner.go: Phantom (sizes, circle/swoop strategy,
//     anchor, cats, PhantomMoveControl/LookControl/BodyRotationControl,
//     travelFlying) and Paper's PhantomSpawner (insomnia, attempts every
//     60..119 s, no creative players; only insomniacs are attacked).
//   - squid.go: Squid and GlowSquid (tentacle strokes, random movement,
//     fleeing, ink, dark ticks).
//   - dolphin.go: Dolphin (air 4800, moistness, Dolphin's Grace, items, jumps,
//     breathing; treasure leading only clears the fish: no structures yet).
//   - silverfish.go: hiding in host blocks, waking friends, InfestedBlock
//     host maps.
//   - creaking.go: Creaking without its heart block (Crossfly has no
//     creaking heart yet): moves only unobserved, activation within 12
//     blocks, heart-bound invulnerability and tear-down through HeartAt.
//   - warden_anger.go, warden_ai.go: the warden's AngerManagement and its
//     Brain behaviours (Emerging, Digging, Roar, Sniffing, SonicBoom,
//     SetRoarTarget, TryToSniff, SetWardenLookTarget, ForceUnmount, the dig
//     cooldown) on package jbrain.
//   - variants.go: rules of the mobs built on the base's zombie, skeleton,
//     spider and the pets row's camel: husk, stray, bogged, parched, cave
//     spider, giant, camel husk.
//
// The functions take *mob.Mob; the base's *javamob.Mob embeds it. Hooks holds
// the base functions this package calls (sounds, line of sight, melee hits,
// movement with collisions); the base points them at its own versions.
package hostile
