//go:build crossfly_full

package event

// Events the engine fires by name (not referenced from any component of the
// definition that receives them). Components that take an event parameter
// (timer, environment_sensor, ageable grow_up, interact, ...) fire whatever
// their JSON names instead.
const (
	// EntitySpawned: natural spawn, spawn egg, /summon without an event, spawner.
	EntitySpawned = "minecraft:entity_spawned"
	// EntityBorn: the baby produced by breeding (fired on the baby).
	EntityBorn = "minecraft:entity_born"
	// EntityTransformed: fired on the result of minecraft:transformation
	// (zombie -> drowned, villager -> witch, ...); "other" is the old entity.
	EntityTransformed = "minecraft:entity_transformed"
	// AgeableGrowUp is the default grow_up event of minecraft:ageable.
	AgeableGrowUp = "minecraft:ageable_grow_up"
	// OnTame is the usual tame_event of minecraft:tameable.
	OnTame = "minecraft:on_tame"
	// SpawnTameAdult: tamed animals spawned by /summon or eggs with an owner.
	SpawnTameAdult = "minecraft:spawn_tame_adult"
	// StartCelebrating: raid won (illagers, witches).
	StartCelebrating = "minecraft:start_celebrating"
	// RaidExpired: raid timed out for raid members.
	RaidExpired = "minecraft:raid_expired"
	// SpawnForRaid: raid wave spawn.
	SpawnForRaid = "minecraft:spawn_for_raid"
	// SpawnFromVillage: villager/golem spawned by a village.
	SpawnFromVillage = "minecraft:spawn_from_village"
	// JoinCaravan / LeaveCaravan: llama caravans.
	JoinCaravan  = "minecraft:join_caravan"
	LeaveCaravan = "minecraft:leave_caravan"
	// PromoteToIllagerCaptain / PromoteToPatrolCaptain: patrol and raid captains.
	PromoteToIllagerCaptain = "minecraft:promote_to_illager_captain"
	PromoteToPatrolCaptain  = "minecraft:promote_to_patrol_captain"
	// SpawnAsIllagerCaptain / SpawnAsPatrolFollower: patrol spawner.
	SpawnAsIllagerCaptain = "minecraft:spawn_as_illager_captain"
	SpawnAsPatrolFollower = "minecraft:spawn_as_patrol_follower"
	// StartSitting / StopSitting: player sitting a pet.
	StartSitting = "minecraft:start_sitting"
	StopSitting  = "minecraft:stop_sitting"
	// SpringTrap: skeleton horse trap triggered by a player.
	SpringTrap = "minecraft:spring_trap"
	// HatchWarm / HatchCold: chicken hatched from a warm/cold egg.
	HatchWarm = "minecraft:hatch_warm"
	HatchCold = "minecraft:hatch_cold"
	// SpawnAsRider: zombie/husk spawned riding a zombie horse / camel husk.
	SpawnAsRider = "minecraft:spawn_as_rider"
	// ExitedHive & co.: bees leaving a hive.
	ExitedHive          = "minecraft:exited_hive"
	ExitedDisturbedHive = "minecraft:exited_disturbed_hive"
	ExitedHiveOnFire    = "minecraft:exited_hive_on_fire"
	HiveFull            = "minecraft:hive_full"
)

// Builtin lists the engine-fired events above.
var Builtin = []string{
	EntitySpawned, EntityBorn, EntityTransformed, AgeableGrowUp, OnTame,
	SpawnTameAdult, StartCelebrating, RaidExpired, SpawnForRaid, SpawnFromVillage,
	JoinCaravan, LeaveCaravan, PromoteToIllagerCaptain, PromoteToPatrolCaptain,
	SpawnAsIllagerCaptain, SpawnAsPatrolFollower, StartSitting, StopSitting,
	SpringTrap, HatchWarm, HatchCold, SpawnAsRider, ExitedHive, ExitedDisturbedHive,
	ExitedHiveOnFire, HiveFull,
}
