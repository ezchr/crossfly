// Package raid is Java Edition's raids and pillager patrols for Crossfly's Java mob mode (mobs=java), as on a Paper
// 26.3 server with its default config: bad omen and raid omen (BadOmenMobEffect, RaidOmenMobEffect), raids
// (net.minecraft.world.entity.raid.Raid and Raids: waves per difficulty, bonus waves by omen level, captains and the
// ominous banner, the boss bar, the horn, victory with hero of the village, loss and the stop reasons), the raid
// fields every raider shares (Raider, PatrollingMonster) and the patrol spawner (PatrolSpawner with Paper's
// pillager-patrols config).
//
// The package is plain logic: it reaches the world through the Level, Raider and Player interfaces, which the Java
// mob mode implements on a world.Tx and its mobs, and the tests implement on fakes. Comments name the Java class
// and method each piece is rewritten from (decompiled vanilla 26.3 and Paper 26.3-244).
package jraid
