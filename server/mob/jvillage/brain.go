//go:build crossfly_full

package jvillage

import (
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/jbrain"
	"github.com/df-mc/dragonfly/server/mob/jbrain/jmob"
	"github.com/df-mc/dragonfly/server/mob/jpoi"
	"github.com/df-mc/dragonfly/server/world"
)

// provider is Villager.BRAIN_PROVIDER (set in init: the activities refer
// back to the villager's brain refresh).
var provider *jbrain.Provider[*javamob.Mob]

func init() { provider = newProvider() }

func newProvider() *jbrain.Provider[*javamob.Mob] {
	return &jbrain.Provider[*javamob.Mob]{
		EntityType: "villager",
		Sensors: []*jbrain.SensorType[*javamob.Mob]{
			jmob.NearestLivingEntities, jmob.NearestPlayers, NearestItems, NearestBed, jmob.HurtBy,
			VillagerHostiles, VillagerBabies, SecondaryPois, GolemDetected,
		},
		Activities: villagerActivities,
	}
}

// villagerActivities is the activity supplier of Villager.BRAIN_PROVIDER.
func villagerActivities(m *javamob.Mob) []jbrain.ActivityData[*javamob.Mob] {
	v, _ := Of(m)
	prof := v.profession()
	var acts []jbrain.ActivityData[*javamob.Mob]
	if v.Baby() {
		acts = append(acts, jbrain.ActivityData[*javamob.Mob]{Activity: jbrain.ActPlay, Behaviours: playPackage(speedModifier)})
	} else {
		acts = append(acts, jbrain.ActivityData[*javamob.Mob]{Activity: jbrain.ActWork, Behaviours: workPackage(prof, speedModifier), Conditions: []jbrain.Req{jbrain.Present(jbrain.MemJobSite)}})
	}
	acts = append(acts,
		jbrain.ActivityData[*javamob.Mob]{Activity: jbrain.ActCore, Behaviours: corePackage(prof, speedModifier)},
		jbrain.ActivityData[*javamob.Mob]{Activity: jbrain.ActMeet, Behaviours: meetPackage(speedModifier), Conditions: []jbrain.Req{jbrain.Present(jbrain.MemMeetingPoint)}},
		jbrain.ActivityData[*javamob.Mob]{Activity: jbrain.ActRest, Behaviours: restPackage(speedModifier)},
		jbrain.ActivityData[*javamob.Mob]{Activity: jbrain.ActIdle, Behaviours: idlePackage(speedModifier)},
		jbrain.ActivityData[*javamob.Mob]{Activity: jbrain.ActPanic, Behaviours: panicPackage(speedModifier)},
		jbrain.ActivityData[*javamob.Mob]{Activity: jbrain.ActPreRaid, Behaviours: preRaidPackage(speedModifier)},
		jbrain.ActivityData[*javamob.Mob]{Activity: jbrain.ActRaid, Behaviours: raidPackage(speedModifier)},
		jbrain.ActivityData[*javamob.Mob]{Activity: jbrain.ActHide, Behaviours: hidePackage(speedModifier)},
	)
	return acts
}

type prio = jbrain.Prioritized[*javamob.Mob]

func p(n int, c jmob.Control) prio { return prio{Priority: n, Control: c} }

func w(c jmob.Control, weight int) jbrain.Weighted[*javamob.Mob] { return jbrain.W(c, weight) }

var event14 = EventHappy

// corePackage is VillagerGoalPackages.getCorePackage.
func corePackage(prof *Profession, speed float32) []prio {
	return []prio{
		p(0, jmob.Swim(0.8)),
		p(0, InteractWithDoor()),
		p(0, jmob.LookAtTargetSink(45, 90)),
		p(0, VillagerPanicTrigger()),
		p(0, WakeUp()),
		p(0, ReactToBell()),
		p(0, SetRaidStatus()),
		p(0, ValidateNearbyPoi(prof.HeldJobSite, jbrain.MemJobSite)),
		p(0, ValidateNearbyPoi(prof.AcquirableJobSite, jbrain.MemPotentialJobSite)),
		p(1, jmob.MoveToTargetSink(150, 250)),
		p(2, PoiCompetitorScan()),
		p(3, LookAndFollowTradingPlayerSink(speed)),
		p(5, GoToWantedItem(func(m *javamob.Mob) bool { v, _ := Of(m); return !v.sleeping }, speed, false, 4)),
		p(6, AcquirePoi(prof.AcquirableJobSite, jbrain.MemJobSite, jbrain.MemPotentialJobSite, true, nil, nil)),
		p(7, GoToPotentialJobSite(speed)),
		p(8, YieldJobSite(speed)),
		p(10, AcquirePoi(jpoi.Is(jpoi.Home), jbrain.MemHome, jbrain.MemHome, false, &event14, validateBedPoi)),
		p(10, AcquirePoi(jpoi.Is(jpoi.Meeting), jbrain.MemMeetingPoint, jbrain.MemMeetingPoint, true, &event14, nil)),
		p(10, AssignProfessionFromJobSite()),
		p(10, ResetProfession()),
	}
}

// workPackage is VillagerGoalPackages.getWorkPackage.
func workPackage(prof *Profession, speed float32) []prio {
	farmer := prof == ProfFarmer
	harvestW, boneW := 5, 7
	if farmer {
		harvestW, boneW = 2, 4
	}
	return []prio{
		minimalLook(),
		p(5, jbrain.RunOne[*javamob.Mob](nil,
			w(WorkAtPoi(farmer), 7),
			w(StrollAroundPoi(jbrain.MemJobSite, 0.4, 4), 2),
			w(StrollToPoi(jbrain.MemJobSite, 0.4, 1, 10), 5),
			w(StrollToPoiList(jbrain.MemSecondaryJobSite, speed, 1, 6, jbrain.MemJobSite), 5),
			w(HarvestFarmland(), harvestW),
			w(UseBonemeal(), boneW),
		)),
		p(10, ShowTradesToPlayer(400, 1600)),
		p(10, jmob.SetLookAndInteract("minecraft:player", 4)),
		p(2, SetWalkTargetFromBlockMemory(jbrain.MemJobSite, speed, 9, 100, 1200)),
		p(3, GiveGiftToHero(100)),
		p(99, jmob.UpdateActivityFromSchedule()),
	}
}

// playPackage is VillagerGoalPackages.getPlayPackage.
func playPackage(speed float32) []prio {
	return []prio{
		p(0, jmob.MoveToTargetSink(80, 120)),
		fullLook(),
		p(5, PlayTagWithOtherKids()),
		p(5, jbrain.RunOne[*javamob.Mob]([]jbrain.Req{jbrain.Absent(jbrain.MemVisibleVillagerBabies)},
			w(jmob.InteractWith(VillagerID, 8, nil, nil, jbrain.MemInteractionTarget, speed, 2), 2),
			w(jmob.InteractWith("minecraft:cat", 8, nil, nil, jbrain.MemInteractionTarget, speed, 2), 1),
			w(VillageBoundRandomStroll(speed, 10, 7), 1),
			w(jmob.SetWalkTargetFromLookTarget(speed, 2), 1),
			w(JumpOnBed(speed), 2),
			w(jmob.DoNothing(20, 40), 2),
		)),
		p(99, jmob.UpdateActivityFromSchedule()),
	}
}

// restPackage is VillagerGoalPackages.getRestPackage.
func restPackage(speed float32) []prio {
	return []prio{
		p(2, SetWalkTargetFromBlockMemory(jbrain.MemHome, speed, 1, 150, 1200)),
		p(3, ValidateNearbyPoi(jpoi.Is(jpoi.Home), jbrain.MemHome)),
		p(3, SleepInBed()),
		p(5, jbrain.RunOne[*javamob.Mob]([]jbrain.Req{jbrain.Absent(jbrain.MemHome)},
			w(SetClosestHomeAsWalkTarget(speed), 1),
			w(InsideBrownianWalk(speed), 4),
			w(GoToClosestVillage(speed, 4), 2),
			w(jmob.DoNothing(20, 40), 2),
		)),
		minimalLook(),
		p(99, jmob.UpdateActivityFromSchedule()),
	}
}

// meetPackage is VillagerGoalPackages.getMeetPackage.
func meetPackage(speed float32) []prio {
	return []prio{
		p(2, jmob.TriggerOneShuffled(
			jmob.WeightedTrigger{T: StrollAroundPoi(jbrain.MemMeetingPoint, 0.4, 40), Weight: 2},
			jmob.WeightedTrigger{T: SocializeAtBell(), Weight: 2},
		)),
		p(10, ShowTradesToPlayer(400, 1600)),
		p(10, jmob.SetLookAndInteract("minecraft:player", 4)),
		p(2, SetWalkTargetFromBlockMemory(jbrain.MemMeetingPoint, speed, 6, 100, 200)),
		p(3, GiveGiftToHero(100)),
		p(3, ValidateNearbyPoi(jpoi.Is(jpoi.Meeting), jbrain.MemMeetingPoint)),
		p(3, jbrain.NewGate[*javamob.Mob](nil, []jbrain.MemoryID{jbrain.MemInteractionTarget.ID()}, jbrain.Ordered, jbrain.RunOnePolicy, []jbrain.Weighted[*javamob.Mob]{w(TradeWithVillager(), 1)})),
		fullLook(),
		p(99, jmob.UpdateActivityFromSchedule()),
	}
}

// idlePackage is VillagerGoalPackages.getIdlePackage.
func idlePackage(speed float32) []prio {
	canBreed := func(m *javamob.Mob) bool { v, ok := Of(m); return ok && v.CanBreed() }
	targetCanBreed := func(e world.Entity) bool {
		jm, ok := javamob.Of(e)
		return ok && canBreed(jm)
	}
	return []prio{
		p(2, jbrain.RunOne[*javamob.Mob](nil,
			w(jmob.InteractWith(VillagerID, 8, nil, nil, jbrain.MemInteractionTarget, speed, 2), 2),
			w(jmob.InteractWith(VillagerID, 8, canBreed, targetCanBreed, jbrain.MemBreedTarget, speed, 2), 1),
			w(jmob.InteractWith("minecraft:cat", 8, nil, nil, jbrain.MemInteractionTarget, speed, 2), 1),
			w(VillageBoundRandomStroll(speed, 10, 7), 1),
			w(jmob.SetWalkTargetFromLookTarget(speed, 2), 1),
			w(JumpOnBed(speed), 1),
			w(jmob.DoNothing(30, 60), 1),
		)),
		p(3, GiveGiftToHero(100)),
		p(3, jmob.SetLookAndInteract("minecraft:player", 4)),
		p(3, ShowTradesToPlayer(400, 1600)),
		p(3, jbrain.NewGate[*javamob.Mob](nil, []jbrain.MemoryID{jbrain.MemInteractionTarget.ID()}, jbrain.Ordered, jbrain.RunOnePolicy, []jbrain.Weighted[*javamob.Mob]{w(TradeWithVillager(), 1)})),
		p(3, jbrain.NewGate[*javamob.Mob](nil, []jbrain.MemoryID{jbrain.MemBreedTarget.ID()}, jbrain.Ordered, jbrain.RunOnePolicy, []jbrain.Weighted[*javamob.Mob]{w(VillagerMakeLove(), 1)})),
		fullLook(),
		p(99, jmob.UpdateActivityFromSchedule()),
	}
}

// panicPackage is VillagerGoalPackages.getPanicPackage.
func panicPackage(speed float32) []prio {
	run := speed * 1.5
	return []prio{
		p(0, VillagerCalmDown()),
		p(1, jmob.SetWalkTargetAwayFromEntity(jbrain.MemNearestHostile, run, 6, false)),
		p(1, jmob.SetWalkTargetAwayFromEntity(jbrain.MemHurtByEntity, run, 6, false)),
		p(3, VillageBoundRandomStroll(run, 2, 2)),
		minimalLook(),
	}
}

// preRaidPackage is VillagerGoalPackages.getPreRaidPackage (bells ring
// through RaidHook's owner; Crossfly has no bell block yet).
func preRaidPackage(speed float32) []prio {
	return []prio{
		p(0, jmob.TriggerOneShuffled(
			jmob.WeightedTrigger{T: setWalkTargetFromBlockMemoryShot(jbrain.MemMeetingPoint, speed*1.5, 2, 150, 200), Weight: 6},
			jmob.WeightedTrigger{T: VillageBoundRandomStroll(speed*1.5, 10, 7), Weight: 2},
		)),
		minimalLook(),
		p(99, ResetRaidStatus()),
	}
}

// raidPackage is VillagerGoalPackages.getRaidPackage, without the
// celebration and sky-seeking parts (the raids package owns raids; they run
// through RaidHook).
func raidPackage(speed float32) []prio {
	return []prio{
		p(0, jbrain.TriggerIf(func(c *jbrain.Ctx, m *javamob.Mob) bool {
			return RaidHook != nil && RaidHook(c.Tx, BlockPos(m)).Victory
		}, VillageBoundRandomStroll(speed*1.1, 10, 7))),
		minimalLook(),
		p(99, ResetRaidStatus()),
	}
}

// hidePackage is VillagerGoalPackages.getHidePackage.
func hidePackage(speed float32) []prio {
	return []prio{
		p(0, SetHiddenState(15, 3)),
		p(1, LocateHidingPlace(32, speed*1.25, 2)),
		minimalLook(),
	}
}

// setWalkTargetFromBlockMemoryShot wraps SetWalkTargetFromBlockMemory as a
// trigger for TriggerGate.
func setWalkTargetFromBlockMemoryShot(mem posMem, speed float32, closeEnough, tooFar, tooLong int) *jbrain.OneShot[*javamob.Mob] {
	return SetWalkTargetFromBlockMemory(mem, speed, closeEnough, tooFar, tooLong).(*jbrain.OneShot[*javamob.Mob])
}

// fullLook is VillagerGoalPackages.getFullLookBehavior.
func fullLook() prio {
	return p(5, jbrain.RunOne[*javamob.Mob](nil,
		w(jmob.SetEntityLookTarget(jmob.OfType("minecraft:cat"), 8), 8),
		w(jmob.SetEntityLookTarget(jmob.OfType(VillagerID), 8), 2),
		w(jmob.SetEntityLookTarget(jmob.OfType("minecraft:player"), 8), 2),
		w(jmob.SetEntityLookTarget(jmob.OfCategory("creature"), 8), 1),
		w(jmob.SetEntityLookTarget(jmob.OfCategory("water_creature"), 8), 1),
		w(jmob.SetEntityLookTarget(jmob.OfCategory("axolotls"), 8), 1),
		w(jmob.SetEntityLookTarget(jmob.OfCategory("underground_water_creature"), 8), 1),
		w(jmob.SetEntityLookTarget(jmob.OfCategory("water_ambient"), 8), 1),
		w(jmob.SetEntityLookTarget(jmob.OfCategory("monster"), 8), 1),
		w(jmob.DoNothing(30, 60), 2),
	))
}

// minimalLook is VillagerGoalPackages.getMinimalLookBehavior.
func minimalLook() prio {
	return p(5, jbrain.RunOne[*javamob.Mob](nil,
		w(jmob.SetEntityLookTarget(jmob.OfType(VillagerID), 8), 2),
		w(jmob.SetEntityLookTarget(jmob.OfType("minecraft:player"), 8), 2),
		w(jmob.DoNothing(30, 60), 8),
	))
}
