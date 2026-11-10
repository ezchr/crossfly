//go:build crossfly_full

// Package animal is the shared animal layer of the Java mob mode (mobs=java): the state and rules of
// Java's AgeableMob and Animal classes (age, growth, feeding, love mode, breeding cooldown, the golden
// dandelion age lock), the animal food tags, and the goals every Java animal uses (BreedGoal, TemptGoal,
// FollowParentGoal, PanicGoal, EatBlockGoal). Farm animals, pets and mounts build on it.
//
// Everything is a rewrite of the vanilla 26.3 classes (Paper 26.3 leaves them as they are with its
// default configuration); comments name the Java class and method each part comes from. Times are in
// ticks.
package animal
