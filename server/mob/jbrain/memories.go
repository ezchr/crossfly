package jbrain

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// Handle is the stored form of Java's entity references in memories: an
// entity handle stays valid across ticks and transactions.
type Handle = *world.EntityHandle

// The vanilla memory module types (MemoryModuleType), in Java's
// registration order, with "minecraft:" names. Entity references are
// handles; types the brain package does not know (Path) are any.
var (
	MemDummy                                 = RegisterMemory[Unit]("minecraft:dummy")
	MemHome                                  = RegisterSavedMemory("minecraft:home", GlobalPosCodec)
	MemJobSite                               = RegisterSavedMemory("minecraft:job_site", GlobalPosCodec)
	MemPotentialJobSite                      = RegisterSavedMemory("minecraft:potential_job_site", GlobalPosCodec)
	MemMeetingPoint                          = RegisterSavedMemory("minecraft:meeting_point", GlobalPosCodec)
	MemSecondaryJobSite                      = RegisterMemory[[]GlobalPos]("minecraft:secondary_job_site")
	MemNearestLivingEntities                 = RegisterMemory[[]Handle]("minecraft:mobs")
	MemNearestVisibleLivingEntities          = RegisterMemory[*VisibleEntities]("minecraft:visible_mobs")
	MemVisibleVillagerBabies                 = RegisterMemory[[]Handle]("minecraft:visible_villager_babies")
	MemNearestPlayers                        = RegisterMemory[[]Handle]("minecraft:nearest_players")
	MemNearestVisiblePlayer                  = RegisterMemory[Handle]("minecraft:nearest_visible_player")
	MemNearestVisibleAttackablePlayer        = RegisterMemory[Handle]("minecraft:nearest_visible_targetable_player")
	MemNearestVisibleAttackablePlayers       = RegisterMemory[[]Handle]("minecraft:nearest_visible_targetable_players")
	MemWalkTarget                            = RegisterMemory[WalkTarget]("minecraft:walk_target")
	MemLookTarget                            = RegisterMemory[PositionTracker]("minecraft:look_target")
	MemAttackTarget                          = RegisterMemory[Handle]("minecraft:attack_target")
	MemAttackCoolingDown                     = RegisterMemory[bool]("minecraft:attack_cooling_down")
	MemInteractionTarget                     = RegisterMemory[Handle]("minecraft:interaction_target")
	MemBreedTarget                           = RegisterMemory[Handle]("minecraft:breed_target")
	MemRideTarget                            = RegisterMemory[Handle]("minecraft:ride_target")
	MemPath                                  = RegisterMemory[any]("minecraft:path")
	MemDoorsToClose                          = RegisterMemory[[]GlobalPos]("minecraft:doors_to_close")
	MemNearestBed                            = RegisterMemory[cube.Pos]("minecraft:nearest_bed")
	MemHurtBy                                = RegisterMemory[world.DamageSource]("minecraft:hurt_by")
	MemHurtByEntity                          = RegisterMemory[Handle]("minecraft:hurt_by_entity")
	MemAvoidTarget                           = RegisterMemory[Handle]("minecraft:avoid_target")
	MemNearestHostile                        = RegisterMemory[Handle]("minecraft:nearest_hostile")
	MemNearestAttackable                     = RegisterMemory[Handle]("minecraft:nearest_attackable")
	MemHidingPlace                           = RegisterMemory[GlobalPos]("minecraft:hiding_place")
	MemHeardBellTime                         = RegisterMemory[int64]("minecraft:heard_bell_time")
	MemCantReachWalkTargetSince              = RegisterMemory[int64]("minecraft:cant_reach_walk_target_since")
	MemGolemDetectedRecently                 = RegisterSavedMemory("minecraft:golem_detected_recently", BoolCodec)
	MemDangerDetectedRecently                = RegisterSavedMemory("minecraft:danger_detected_recently", BoolCodec)
	MemLastSlept                             = RegisterSavedMemory("minecraft:last_slept", LongCodec)
	MemLastWoken                             = RegisterSavedMemory("minecraft:last_woken", LongCodec)
	MemLastWorkedAtPoi                       = RegisterSavedMemory("minecraft:last_worked_at_poi", LongCodec)
	MemNearestVisibleAdult                   = RegisterMemory[Handle]("minecraft:nearest_visible_adult")
	MemNearestVisibleWantedItem              = RegisterMemory[Handle]("minecraft:nearest_visible_wanted_item")
	MemNearestVisibleNemesis                 = RegisterMemory[Handle]("minecraft:nearest_visible_nemesis")
	MemPlayDeadTicks                         = RegisterSavedMemory("minecraft:play_dead_ticks", IntCodec)
	MemTemptingPlayer                        = RegisterMemory[Handle]("minecraft:tempting_player")
	MemTemptationCooldownTicks               = RegisterSavedMemory("minecraft:temptation_cooldown_ticks", IntCodec)
	MemGazeCooldownTicks                     = RegisterSavedMemory("minecraft:gaze_cooldown_ticks", IntCodec)
	MemLongJumpCooldownTicks                 = RegisterSavedMemory("minecraft:long_jump_cooling_down", IntCodec)
	MemLongJumpMidJump                       = RegisterMemory[bool]("minecraft:long_jump_mid_jump")
	MemHasHuntingCooldown                    = RegisterSavedMemory("minecraft:has_hunting_cooldown", BoolCodec)
	MemRamCooldownTicks                      = RegisterSavedMemory("minecraft:ram_cooldown_ticks", IntCodec)
	MemRamTarget                             = RegisterMemory[mgl64.Vec3]("minecraft:ram_target")
	MemIsInWater                             = RegisterSavedMemory("minecraft:is_in_water", UnitCodec)
	MemIsPregnant                            = RegisterSavedMemory("minecraft:is_pregnant", UnitCodec)
	MemIsPanicking                           = RegisterSavedMemory("minecraft:is_panicking", BoolCodec)
	MemUnreachableTongueTargets              = RegisterMemory[[]uuid.UUID]("minecraft:unreachable_tongue_targets")
	MemVisitedBlockPositions                 = RegisterMemory[[]GlobalPos]("minecraft:visited_block_positions")
	MemUnreachableTransportBlockPositions    = RegisterMemory[[]GlobalPos]("minecraft:unreachable_transport_block_positions")
	MemTransportItemsCooldownTicks           = RegisterMemory[int]("minecraft:transport_items_cooldown_ticks")
	MemChargeCooldownTicks                   = RegisterSavedMemory("minecraft:charge_cooldown_ticks", IntCodec)
	MemAttackTargetCooldown                  = RegisterSavedMemory("minecraft:attack_target_cooldown", IntCodec)
	MemSpearFleeingTime                      = RegisterMemory[int]("minecraft:spear_fleeing_time")
	MemSpearFleeingPosition                  = RegisterMemory[mgl64.Vec3]("minecraft:spear_fleeing_position")
	MemSpearChargePosition                   = RegisterMemory[mgl64.Vec3]("minecraft:spear_charge_position")
	MemSpearEngageTime                       = RegisterMemory[int]("minecraft:spear_engage_time")
	MemSpearStatus                           = RegisterMemory[int]("minecraft:spear_status")
	MemAngryAt                               = RegisterSavedMemory("minecraft:angry_at", UUIDCodec)
	MemUniversalAnger                        = RegisterSavedMemory("minecraft:universal_anger", BoolCodec)
	MemAdmiringItem                          = RegisterSavedMemory("minecraft:admiring_item", BoolCodec)
	MemTimeTryingToReachAdmireItem           = RegisterMemory[int]("minecraft:time_trying_to_reach_admire_item")
	MemDisableWalkToAdmireItem               = RegisterMemory[bool]("minecraft:disable_walk_to_admire_item")
	MemAdmiringDisabled                      = RegisterSavedMemory("minecraft:admiring_disabled", BoolCodec)
	MemHuntedRecently                        = RegisterSavedMemory("minecraft:hunted_recently", BoolCodec)
	MemCelebrateLocation                     = RegisterMemory[cube.Pos]("minecraft:celebrate_location")
	MemDancing                               = RegisterMemory[bool]("minecraft:dancing")
	MemNearestVisibleHuntableHoglin          = RegisterMemory[Handle]("minecraft:nearest_visible_huntable_hoglin")
	MemNearestVisibleBabyHoglin              = RegisterMemory[Handle]("minecraft:nearest_visible_baby_hoglin")
	MemNearestTargetablePlayerNotWearingGold = RegisterMemory[Handle]("minecraft:nearest_targetable_player_not_wearing_gold")
	MemNearbyAdultPiglins                    = RegisterMemory[[]Handle]("minecraft:nearby_adult_piglins")
	MemNearestVisibleAdultPiglins            = RegisterMemory[[]Handle]("minecraft:nearest_visible_adult_piglins")
	MemNearestVisibleAdultHoglins            = RegisterMemory[[]Handle]("minecraft:nearest_visible_adult_hoglins")
	MemNearestVisibleAdultPiglin             = RegisterMemory[Handle]("minecraft:nearest_visible_adult_piglin")
	MemNearestVisibleZombified               = RegisterMemory[Handle]("minecraft:nearest_visible_zombified")
	MemVisibleAdultPiglinCount               = RegisterMemory[int]("minecraft:visible_adult_piglin_count")
	MemVisibleAdultHoglinCount               = RegisterMemory[int]("minecraft:visible_adult_hoglin_count")
	MemNearestPlayerHoldingWantedItem        = RegisterMemory[Handle]("minecraft:nearest_player_holding_wanted_item")
	MemAteRecently                           = RegisterMemory[bool]("minecraft:ate_recently")
	MemNearestRepellent                      = RegisterMemory[cube.Pos]("minecraft:nearest_repellent")
	MemPacified                              = RegisterMemory[bool]("minecraft:pacified")
	MemRoarTarget                            = RegisterMemory[Handle]("minecraft:roar_target")
	MemDisturbanceLocation                   = RegisterMemory[cube.Pos]("minecraft:disturbance_location")
	MemRecentProjectile                      = RegisterSavedMemory("minecraft:recent_projectile", UnitCodec)
	MemIsSniffing                            = RegisterSavedMemory("minecraft:is_sniffing", UnitCodec)
	MemIsEmerging                            = RegisterSavedMemory("minecraft:is_emerging", UnitCodec)
	MemRoarSoundDelay                        = RegisterSavedMemory("minecraft:roar_sound_delay", UnitCodec)
	MemDigCooldown                           = RegisterSavedMemory("minecraft:dig_cooldown", UnitCodec)
	MemRoarSoundCooldown                     = RegisterSavedMemory("minecraft:roar_sound_cooldown", UnitCodec)
	MemSniffCooldown                         = RegisterSavedMemory("minecraft:sniff_cooldown", UnitCodec)
	MemTouchCooldown                         = RegisterSavedMemory("minecraft:touch_cooldown", UnitCodec)
	MemVibrationCooldown                     = RegisterSavedMemory("minecraft:vibration_cooldown", UnitCodec)
	MemSonicBoomCooldown                     = RegisterSavedMemory("minecraft:sonic_boom_cooldown", UnitCodec)
	MemSonicBoomSoundCooldown                = RegisterSavedMemory("minecraft:sonic_boom_sound_cooldown", UnitCodec)
	MemSonicBoomSoundDelay                   = RegisterSavedMemory("minecraft:sonic_boom_sound_delay", UnitCodec)
	MemLikedPlayer                           = RegisterSavedMemory("minecraft:liked_player", UUIDCodec)
	MemLikedNoteblockPosition                = RegisterSavedMemory("minecraft:liked_noteblock", GlobalPosCodec)
	MemLikedNoteblockCooldownTicks           = RegisterSavedMemory("minecraft:liked_noteblock_cooldown_ticks", IntCodec)
	MemItemPickupCooldownTicks               = RegisterSavedMemory("minecraft:item_pickup_cooldown_ticks", IntCodec)
	MemSnifferExploredPositions              = RegisterSavedMemory("minecraft:sniffer_explored_positions", Codec[[]GlobalPos]{Encode: encodeGlobalPosList, Decode: decodeGlobalPosList})
	MemSnifferSniffingTarget                 = RegisterMemory[cube.Pos]("minecraft:sniffer_sniffing_target")
	MemSnifferDigging                        = RegisterMemory[bool]("minecraft:sniffer_digging")
	MemSnifferHappy                          = RegisterMemory[bool]("minecraft:sniffer_happy")
	MemBreezeJumpCooldown                    = RegisterSavedMemory("minecraft:breeze_jump_cooldown", UnitCodec)
	MemBreezeShoot                           = RegisterSavedMemory("minecraft:breeze_shoot", UnitCodec)
	MemBreezeShootCharging                   = RegisterSavedMemory("minecraft:breeze_shoot_charging", UnitCodec)
	MemBreezeShootRecovering                 = RegisterSavedMemory("minecraft:breeze_shoot_recover", UnitCodec)
	MemBreezeShootCooldown                   = RegisterSavedMemory("minecraft:breeze_shoot_cooldown", UnitCodec)
	MemBreezeJumpInhaling                    = RegisterSavedMemory("minecraft:breeze_jump_inhaling", UnitCodec)
	MemBreezeJumpTarget                      = RegisterSavedMemory("minecraft:breeze_jump_target", BlockPosCodec)
	MemBreezeLeavingWater                    = RegisterSavedMemory("minecraft:breeze_leaving_water", UnitCodec)
)

func encodeGlobalPosList(l []GlobalPos) any {
	out := make([]any, len(l))
	for i, g := range l {
		out[i] = GlobalPosCodec.Encode(g)
	}
	return out
}

func decodeGlobalPosList(v any) ([]GlobalPos, bool) {
	var out []GlobalPos
	switch t := v.(type) {
	case []any:
		for _, x := range t {
			g, ok := GlobalPosCodec.Decode(x)
			if !ok {
				return nil, false
			}
			out = append(out, g)
		}
	case []map[string]any:
		for _, x := range t {
			g, ok := GlobalPosCodec.Decode(x)
			if !ok {
				return nil, false
			}
			out = append(out, g)
		}
	default:
		return nil, false
	}
	return out, true
}
