package jraid

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// Level is what raids, raiders, omens and patrols need of a world (ServerLevel). The Java mob mode implements it
// on a world.Tx; tests implement it on a fake.
type Level interface {
	// Difficulty is the world's difficulty (ServerLevel.getDifficulty).
	Difficulty() Difficulty
	// DifficultyAt is ServerLevel.getCurrentDifficultyAt: the regional difficulty at pos.
	DifficultyAt(pos cube.Pos) DifficultyInstance
	// GameTime is the level's game time (Level.getGameTime).
	GameTime() int64
	// RaidsEnabled is the raids game rule (GameRules.RAIDS).
	RaidsEnabled() bool
	// MobGriefing is the mob_griefing game rule.
	MobGriefing() bool
	// CanStartRaid is the minecraft:gameplay/can_start_raid environment attribute at pos (false in the nether).
	CanStartRaid(pos cube.Pos) bool
	// SectionsToVillage is PoiManager.sectionsToVillage(SectionPos.of(pos)): the Chebyshev distance in sub chunks
	// from pos to the nearest sub chunk with an occupied village point of interest (a job site, bed or bell
	// claimed by a villager), at most 7.
	SectionsToVillage(pos cube.Pos) int
	// OccupiedVillagePOI is PoiManager.getInRange(PoiTypeTags.VILLAGE, pos, radius, IS_OCCUPIED): the positions
	// of the occupied village points of interest within radius blocks of pos.
	OccupiedVillagePOI(pos cube.Pos, radius int) []cube.Pos
	// ChunkLoaded is Level.hasChunkAt.
	ChunkLoaded(pos cube.Pos) bool
	// ChunksLoaded is Level.hasChunksAt(minX, minZ, maxX, maxZ).
	ChunksLoaded(minX, minZ, maxX, maxZ int) bool
	// EntityTicking is ServerLevel.isPositionEntityTicking.
	EntityTicking(pos cube.Pos) bool
	// SurfaceY is Level.getHeight(Heightmap.Types.WORLD_SURFACE, x, z): the y above the highest non-air block.
	SurfaceY(x, z int) int
	// RavagerSpawnPositionOK is Raid.RAVAGER_SPAWN_PLACEMENT_TYPE.isSpawnPositionOk(level, pos, RAVAGER), or snow
	// below and air at pos (Raid.findRandomSpawnPos).
	RavagerSpawnPositionOK(pos cube.Pos) bool
	// NewRaider is EntityType.create(level, EntitySpawnReason.EVENT) for the kind: a raider not yet in the world,
	// or nil.
	NewRaider(k Kind) Raider
	// Players are the players of the level (ServerLevel.players()).
	Players() []Player
	// Hero is ServerLevel.getEntity(id) for a hero of the village: false if it is not in the level.
	Hero(id uuid.UUID) (Hero, bool)
	// EntityLoaded is ServerLevel.getEntity(id) != null.
	EntityLoaded(id uuid.UUID) bool
	// SameLevel reports whether the raider is in this level (raider.level().dimension() == level.dimension()).
	SameLevel(r Raider) bool
}

// IsVillage is ServerLevel.isVillage: pos is at most one sub chunk away from a village sub chunk.
func IsVillage(l Level, pos cube.Pos) bool { return IsCloseToVillage(l, pos, 1) }

// IsCloseToVillage is ServerLevel.isCloseToVillage.
func IsCloseToVillage(l Level, pos cube.Pos, sectionDistance int) bool {
	return sectionDistance <= 6 && l.SectionsToVillage(pos) <= sectionDistance
}

// Raider is a raider mob (net.minecraft.world.entity.raid.Raider) as the raid code sees it.
type Raider interface {
	// RaiderState returns the raid and patrol fields of the raider.
	RaiderState() *State
	// UUID is the raider's UUID.
	UUID() uuid.UUID
	// CanBeLeader is PatrollingMonster.canBeLeader.
	CanBeLeader() bool
	// Health is the raider's health.
	Health() float32
	// Alive is LivingEntity.isAlive.
	Alive() bool
	// Removed is Entity.isRemoved.
	Removed() bool
	// BlockPos is Entity.blockPosition.
	BlockPos() cube.Pos
	// TickCount is Entity.tickCount.
	TickCount() int
	// NoActionTime is LivingEntity.getNoActionTime.
	NoActionTime() int
	// Celebrate is Raider.setCelebrating.
	Celebrate(on bool)
	// SetCaptainBanner puts the ominous banner on the raider's head with a drop chance of 2 (Raid.setLeader).
	SetCaptainBanner()
	// SpawnForRaid is the part of Raid.joinRaid for a new raider: at pos (x+0.5, y+1, z+0.5) it is finalized
	// (finalizeSpawn, EntitySpawnReason.EVENT), gets its raid buffs for the wave (applyRaidBuffs), is put on the
	// ground and added to the world with its passengers.
	SpawnForRaid(pos cube.Pos, wave int)
	// RideForRaid is snapTo(pos) and startRiding(vehicle) for the rider of a raid's ravager.
	RideForRaid(vehicle Raider, pos cube.Pos)
}

// Player is a player as raids and omens see it.
type Player interface {
	UUID() uuid.UUID
	Alive() bool
	Spectator() bool
	BlockPos() cube.Pos
	Position() mgl64.Vec3
	// ShowRaidBar shows the raid's boss bar to the player, or updates it.
	ShowRaidBar(id uuid.UUID, b Bar)
	// HideRaidBar hides the raid's boss bar.
	HideRaidBar(id uuid.UUID)
	// PlayRaidHorn plays the raid horn (event.raid.horn, NEUTRAL, volume 64) to the player only at pos.
	PlayRaidHorn(pos mgl64.Vec3)
	// RaidOmen returns the amplifier of the player's raid omen effect.
	RaidOmen() (amplifier int, ok bool)
	// AddRaidOmen gives the player raid omen with the amplifier for ticks.
	AddRaidOmen(amplifier, ticks int)
	// RemoveBadOmen and RemoveRaidOmen remove the effects.
	RemoveBadOmen()
	RemoveRaidOmen()
}

// Hero is an entity that killed a raider of a raid it may win.
type Hero interface {
	Spectator() bool
	// AddHeroOfTheVillage gives the hero of the village effect for ticks, without particles, with an icon.
	AddHeroOfTheVillage(amplifier, ticks int)
}
