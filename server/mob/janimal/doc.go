//go:build crossfly_full

// Package janimal is the shared animal layer of the Java mob mode (mobs=java): the state and rules of
// Java's AgeableMob and Animal classes (age, growth, feeding, love mode, breeding cooldown, the golden
// dandelion age lock), the animal food tags, and the goals every Java animal uses (BreedGoal, TemptGoal,
// FollowParentGoal, PanicGoal, EatBlockGoal). Farm animals (jfarm), pets and mounts (jpets) build on it.
//
// Everything is a rewrite of the vanilla 26.3 classes (Paper 26.3 leaves them as they are with its
// default configuration); comments name the Java class and method each part comes from. Times are in
// ticks.
//
// # Using it
//
//   - Keep an Animal in the mob's state (Ageable alone for an AgeableMob that is not an Animal). Call
//     BeforeAI before the goal selector (Animal.customServerAiStep) and Tick after the AI
//     (AgeableMob/Animal.aiStep); a Boundary result means the baby flag changed. ResetLove when hurt.
//   - Interaction: Feed(isFood, player) (FeedIf for classes overriding canFallInLove) says whether the
//     item is used, the animal fell in love (broadcast Event(EventLoveHearts)) or the baby grew; then
//     CanUseGoldenDandelion/ToggleAgeLock for the golden dandelion (AgeableMob.mobInteract).
//   - Goals take a Host[H] (the mob's adapter onto the Java mob base) on every call: BreedGoal (CanMate
//     overrides Animal.canMate; Host.BreedOffspring is getBreedOffspring), NewTemptGoal with StackIn(tag),
//     FollowParentGoal, PanicGoal (ShouldPanic overrides shouldPanic), EatBlockGoal.
//   - Save data: EncodeNBT/DecodeNBT write the Java keys (Age, ForcedAge, AgeLocked, InLove, LoveCause).
package janimal
