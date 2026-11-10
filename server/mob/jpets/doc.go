//go:build crossfly_full

// Package jpets holds the pets and mounts of the Java mob mode (mobs=java):
// taming, owners, sitting and following (TamableAnimal and its goals), as on
// a Paper 26.3 server with its default configuration.
//
// Java types registered here: wolf, cat, ocelot, horse, donkey, mule,
// skeleton horse, zombie horse, llama, polar bear, fox and panda. Parrots,
// allays and happy ghasts (flying navigation), axolotls and nautiluses
// (water navigation), striders (lava walking) and camels (the Brain's
// shared behaviours) keep their Bedrock-engine type until the base has
// what they need.
//
// Every rule cites the Java class and method it is rewritten from (decompiled
// vanilla 26.3, net.minecraft.world.entity...; Paper's changes are named
// where it changes behaviour). The Bedrock mob engine (server/mob with the
// behaviour packs) does not use this package.
//
// Files: tame.go and tamable.go (TamableAnimal, its goals), mount.go (the
// riding component crossfly:java_mount for both editions' riders), host.go
// (the janimal glue), hurt.go (who hurt whom), variant.go (data-driven
// variants), dye.go, genes.go, horse_stats.go (pure rules), then one or two
// files per mob.
package jpets
