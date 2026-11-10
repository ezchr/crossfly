//go:build crossfly_full

// Package jpets holds the pets and mounts of the Java mob mode (mobs=java):
// taming, owners, sitting and following (TamableAnimal and its goals), wolves,
// cats, ocelots, parrots, foxes, pandas, polar bears, the horse family
// (horse, donkey, mule, skeleton and zombie horse), llamas, camels, striders,
// axolotls, allays, happy ghasts and nautiluses, as on a Paper 26.3 server with
// its default configuration.
//
// Every rule cites the Java class and method it is rewritten from (decompiled
// vanilla 26.3, net.minecraft.world.entity...). The Bedrock mob engine
// (server/mob with the behaviour packs) does not use this package.
//
// The files without a mob in their name are the parts shared by several mobs:
// owner state and teleporting (tame.go), the variant registries with their
// spawn conditions (variant.go), dye mixing (dye.go) and the horse stats
// (horse_stats.go).
package jpets
