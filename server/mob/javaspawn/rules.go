//go:build crossfly_full

package javaspawn

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Placement is a SpawnPlacementType (SpawnPlacementTypes).
type Placement uint8

const (
	NoRestrictions Placement = iota
	OnGround
	InWater
	InLava
)

// spawnPredicate is a SpawnPlacements.SpawnPredicate for
// EntitySpawnReason.NATURAL.
type spawnPredicate func(e *env, t *EntityType, pos cube.Pos) bool

type placementData struct {
	placement Placement
	predicate spawnPredicate
}

// spawnPlacements is the SpawnPlacements static registration of 26.3: the
// placement type and spawn predicate of every type that has one. Types not
// listed have NO_RESTRICTIONS and no predicate. The heightmap type only
// matters for chunk generation spawns, which Crossfly does not do.
var spawnPlacements = map[string]placementData{
	"minecraft:axolotl":          {InWater, checkAxolotlSpawnRules},
	"minecraft:cod":              {InWater, checkSurfaceWaterAnimalSpawnRules},
	"minecraft:dolphin":          {InWater, checkSurfaceWaterAnimalSpawnRules}, // AgeableWaterCreature.checkSurfaceAgeableWaterCreatureSpawnRules: same body
	"minecraft:drowned":          {InWater, checkDrownedSpawnRules},
	"minecraft:guardian":         {InWater, checkGuardianSpawnRules},
	"minecraft:pufferfish":       {InWater, checkSurfaceWaterAnimalSpawnRules},
	"minecraft:salmon":           {InWater, checkSurfaceWaterAnimalSpawnRules},
	"minecraft:squid":            {InWater, checkSurfaceWaterAnimalSpawnRules},
	"minecraft:tropical_fish":    {InWater, checkTropicalFishSpawnRules},
	"minecraft:armadillo":        {OnGround, tagBright("armadillo_spawnable_on")},
	"minecraft:bat":              {OnGround, checkBatSpawnRules},
	"minecraft:blaze":            {OnGround, checkAnyLightMonsterSpawnRules},
	"minecraft:bogged":           {OnGround, checkMonsterSpawnRules},
	"minecraft:breeze":           {OnGround, checkAnyLightMonsterSpawnRules},
	"minecraft:camel":            {OnGround, tagBright("camels_spawnable_on")},
	"minecraft:camel_husk":       {OnGround, checkSurfaceMonstersSpawnRules},
	"minecraft:cave_spider":      {OnGround, checkMonsterSpawnRules},
	"minecraft:chicken":          {OnGround, checkAnimalSpawnRules},
	"minecraft:cow":              {OnGround, checkAnimalSpawnRules},
	"minecraft:creeper":          {OnGround, checkMonsterSpawnRules},
	"minecraft:donkey":           {OnGround, checkAnimalSpawnRules},
	"minecraft:enderman":         {OnGround, checkMonsterSpawnRules},
	"minecraft:endermite":        {OnGround, checkEndermiteSpawnRules},
	"minecraft:ender_dragon":     {OnGround, checkMobSpawnRules},
	"minecraft:frog":             {OnGround, tagBright("frogs_spawnable_on")},
	"minecraft:ghast":            {OnGround, checkGhastSpawnRules},
	"minecraft:happy_ghast":      {OnGround, checkAnimalSpawnRules},
	"minecraft:giant":            {OnGround, checkMonsterSpawnRules},
	"minecraft:glow_squid":       {InWater, checkGlowSquidSpawnRules},
	"minecraft:goat":             {OnGround, tagBright("goats_spawnable_on")},
	"minecraft:horse":            {OnGround, checkAnimalSpawnRules},
	"minecraft:husk":             {OnGround, checkSurfaceMonstersSpawnRules},
	"minecraft:iron_golem":       {OnGround, checkMobSpawnRules},
	"minecraft:llama":            {OnGround, checkAnimalSpawnRules},
	"minecraft:magma_cube":       {OnGround, checkMagmaCubeSpawnRules},
	"minecraft:sulfur_cube":      {OnGround, alwaysSpawn},
	"minecraft:mooshroom":        {OnGround, tagBright("mooshrooms_spawnable_on")},
	"minecraft:mule":             {OnGround, checkAnimalSpawnRules},
	"minecraft:nautilus":         {InWater, checkNautilusSpawnRules},
	"minecraft:ocelot":           {OnGround, checkOcelotSpawnRules},
	"minecraft:parrot":           {OnGround, tagBright("parrots_spawnable_on")},
	"minecraft:pig":              {OnGround, checkAnimalSpawnRules},
	"minecraft:hoglin":           {OnGround, notOnNetherWart},
	"minecraft:piglin":           {OnGround, notOnNetherWart},
	"minecraft:pillager":         {OnGround, checkPatrollingMonsterSpawnRules},
	"minecraft:polar_bear":       {OnGround, checkPolarBearSpawnRules},
	"minecraft:rabbit":           {OnGround, tagBright("rabbits_spawnable_on")},
	"minecraft:sheep":            {OnGround, checkAnimalSpawnRules},
	"minecraft:silverfish":       {OnGround, checkEndermiteSpawnRules}, // Silverfish.checkSilverfishSpawnRules: same body
	"minecraft:skeleton":         {OnGround, checkMonsterSpawnRules},
	"minecraft:skeleton_horse":   {OnGround, checkAnimalSpawnRules}, // SkeletonHorse.checkSkeletonHorseSpawnRules for non-spawner reasons
	"minecraft:slime":            {OnGround, checkSlimeSpawnRules},
	"minecraft:snow_golem":       {OnGround, checkMobSpawnRules},
	"minecraft:spider":           {OnGround, checkMonsterSpawnRules},
	"minecraft:stray":            {OnGround, checkStraySpawnRules},
	"minecraft:parched":          {OnGround, checkSurfaceMonstersSpawnRules},
	"minecraft:strider":          {InLava, checkStriderSpawnRules},
	"minecraft:turtle":           {OnGround, checkTurtleSpawnRules},
	"minecraft:villager":         {OnGround, checkMobSpawnRules},
	"minecraft:witch":            {OnGround, checkMonsterSpawnRules},
	"minecraft:wither":           {OnGround, checkMonsterSpawnRules},
	"minecraft:wither_skeleton":  {OnGround, checkMonsterSpawnRules},
	"minecraft:wolf":             {OnGround, tagBright("wolves_spawnable_on")},
	"minecraft:zoglin":           {OnGround, checkAnyLightMonsterSpawnRules},
	"minecraft:creaking":         {OnGround, checkMonsterSpawnRules},
	"minecraft:zombie":           {OnGround, checkMonsterSpawnRules},
	"minecraft:zombie_horse":     {OnGround, checkMonsterSpawnRules},
	"minecraft:zombified_piglin": {OnGround, checkZombifiedPiglinSpawnRules},
	"minecraft:zombie_villager":  {OnGround, checkMonsterSpawnRules},
	"minecraft:cat":              {OnGround, checkAnimalSpawnRules},
	"minecraft:elder_guardian":   {InWater, checkGuardianSpawnRules},
	"minecraft:evoker":           {NoRestrictions, checkMonsterSpawnRules},
	"minecraft:fox":              {NoRestrictions, tagBright("foxes_spawnable_on")},
	"minecraft:illusioner":       {NoRestrictions, checkMonsterSpawnRules},
	"minecraft:panda":            {NoRestrictions, checkAnimalSpawnRules},
	"minecraft:phantom":          {NoRestrictions, checkMobSpawnRules},
	"minecraft:ravager":          {OnGround, checkMonsterSpawnRules},
	"minecraft:shulker":          {NoRestrictions, checkMobSpawnRules},
	"minecraft:trader_llama":     {NoRestrictions, checkAnimalSpawnRules},
	"minecraft:vex":              {NoRestrictions, checkMonsterSpawnRules},
	"minecraft:vindicator":       {NoRestrictions, checkMonsterSpawnRules},
	"minecraft:wandering_trader": {OnGround, checkMobSpawnRules},
	"minecraft:warden":           {NoRestrictions, checkMonsterSpawnRules},
}

// PlacementOf returns the SpawnPlacements placement type of a Java type.
func PlacementOf(typ string) Placement { return spawnPlacements[typ].placement }

// isSpawnPositionOk is SpawnPlacementType.isSpawnPositionOk.
func (e *env) isSpawnPositionOk(t *EntityType, pos cube.Pos) bool {
	switch spawnPlacements[t.ID].placement {
	case OnGround:
		below := pos.Side(cube.FaceDown)
		return isValidSpawn(e.tx, below, t) && isValidEmptySpawnBlock(e.tx, pos, t) &&
			isValidEmptySpawnBlock(e.tx, pos.Side(cube.FaceUp), t)
	case InWater:
		w, _ := fluidAt(e.tx, pos)
		above := pos.Side(cube.FaceUp)
		return w && !isRedstoneConductor(e.tx, above, e.tx.Block(above))
	case InLava:
		_, l := fluidAt(e.tx, pos)
		return l
	}
	return true
}

// checkSpawnRules is SpawnPlacements.checkSpawnRules for a natural spawn.
func (e *env) checkSpawnRules(t *EntityType, pos cube.Pos) bool {
	if !t.AllowedInPeaceful && e.peaceful() {
		return false
	}
	d, ok := spawnPlacements[t.ID]
	return !ok || d.predicate(e, t, pos)
}

func alwaysSpawn(*env, *EntityType, cube.Pos) bool { return true }

// checkMobSpawnRules is Mob.checkMobSpawnRules.
func checkMobSpawnRules(e *env, t *EntityType, pos cube.Pos) bool {
	return e.spawner || isValidSpawn(e.tx, pos.Side(cube.FaceDown), t)
}

// checkMonsterSpawnRules is Monster.checkMonsterSpawnRules.
func checkMonsterSpawnRules(e *env, t *EntityType, pos cube.Pos) bool {
	return e.isDarkEnoughToSpawn(pos) && checkMobSpawnRules(e, t, pos)
}

// checkAnyLightMonsterSpawnRules is Monster.checkAnyLightMonsterSpawnRules.
func checkAnyLightMonsterSpawnRules(e *env, t *EntityType, pos cube.Pos) bool {
	return checkMobSpawnRules(e, t, pos)
}

// checkSurfaceMonstersSpawnRules is Monster.checkSurfaceMonstersSpawnRules
// (husk, parched, camel husk).
func checkSurfaceMonstersSpawnRules(e *env, t *EntityType, pos cube.Pos) bool {
	return checkMonsterSpawnRules(e, t, pos) && (e.spawner || e.canSeeSky(pos))
}

// isBrightEnoughToSpawn is Animal.isBrightEnoughToSpawn:
// getRawBrightness(pos, 0) > 8, without the time of day.
func (e *env) isBrightEnoughToSpawn(pos cube.Pos) bool { return e.rawBrightness(pos, 0) > 8 }

// checkAnimalSpawnRules is Animal.checkAnimalSpawnRules.
func checkAnimalSpawnRules(e *env, _ *EntityType, pos cube.Pos) bool {
	return e.hasTag(pos.Side(cube.FaceDown), "animals_spawnable_on") && e.isBrightEnoughToSpawn(pos)
}

// tagBright is the "below in #tag and bright enough" rule of Frog,
// MushroomCow, Parrot, Rabbit, Fox, Armadillo, Wolf, Goat and Camel.
func tagBright(tag string) spawnPredicate {
	return func(e *env, _ *EntityType, pos cube.Pos) bool {
		return e.hasTag(pos.Side(cube.FaceDown), tag) && e.isBrightEnoughToSpawn(pos)
	}
}

// checkAxolotlSpawnRules is Axolotl.checkAxolotlSpawnRules.
func checkAxolotlSpawnRules(e *env, _ *EntityType, pos cube.Pos) bool {
	return e.hasTag(pos.Side(cube.FaceDown), "axolotls_spawnable_on")
}

// checkSurfaceWaterAnimalSpawnRules is WaterAnimal.checkSurfaceWaterAnimalSpawnRules
// (and AgeableWaterCreature.checkSurfaceAgeableWaterCreatureSpawnRules):
// sea level - 13 .. sea level, water below and a water block above.
func checkSurfaceWaterAnimalSpawnRules(e *env, _ *EntityType, pos cube.Pos) bool {
	w, _ := fluidAt(e.tx, pos.Side(cube.FaceDown))
	return pos[1] >= e.seaLevel-13 && pos[1] <= e.seaLevel && w && isWaterBlock(e.tx, pos.Side(cube.FaceUp))
}

// checkNautilusSpawnRules is AbstractNautilus.checkNautilusSpawnRules.
func checkNautilusSpawnRules(e *env, _ *EntityType, pos cube.Pos) bool {
	w, _ := fluidAt(e.tx, pos.Side(cube.FaceDown))
	return pos[1] >= e.seaLevel-25 && pos[1] <= e.seaLevel-5 && w && isWaterBlock(e.tx, pos.Side(cube.FaceUp))
}

// checkTropicalFishSpawnRules is TropicalFish.checkTropicalFishSpawnRules.
func checkTropicalFishSpawnRules(e *env, t *EntityType, pos cube.Pos) bool {
	w, _ := fluidAt(e.tx, pos.Side(cube.FaceDown))
	return w && isWaterBlock(e.tx, pos.Side(cube.FaceUp)) &&
		(e.biomeAt(pos).tags["allows_tropical_fish_spawns_at_any_height"] || checkSurfaceWaterAnimalSpawnRules(e, t, pos))
}

// checkGlowSquidSpawnRules is GlowSquid.checkGlowSquidSpawnRules.
func checkGlowSquidSpawnRules(e *env, _ *EntityType, pos cube.Pos) bool {
	return pos[1] <= e.seaLevel-33 && e.rawBrightness(pos, 0) == 0 && isWaterBlock(e.tx, pos)
}

// checkTurtleSpawnRules is Turtle.checkTurtleSpawnRules (TurtleEggBlock.onSand).
func checkTurtleSpawnRules(e *env, _ *EntityType, pos cube.Pos) bool {
	return pos[1] < e.seaLevel+4 && e.hasTag(pos.Side(cube.FaceDown), "sand") && e.isBrightEnoughToSpawn(pos)
}

// checkPolarBearSpawnRules is PolarBear.checkPolarBearSpawnRules.
func checkPolarBearSpawnRules(e *env, t *EntityType, pos cube.Pos) bool {
	if !e.biomeAt(pos).tags["polar_bears_spawn_on_alternate_blocks"] {
		return checkAnimalSpawnRules(e, t, pos)
	}
	return e.isBrightEnoughToSpawn(pos) && e.hasTag(pos.Side(cube.FaceDown), "polar_bears_spawnable_on_alternate")
}

// checkOcelotSpawnRules is Ocelot.checkOcelotSpawnRules.
func checkOcelotSpawnRules(e *env, _ *EntityType, _ cube.Pos) bool { return e.rnd.IntN(3) != 0 }

// checkBatSpawnRules is Bat.checkBatSpawnRules: under the WORLD_SURFACE
// heightmap, half of the attempts, darker than a random 0..3.
func checkBatSpawnRules(e *env, t *EntityType, pos cube.Pos) bool {
	if pos[1] >= e.worldSurface(pos[0], pos[2]) {
		return false
	}
	if e.rnd.IntN(2) == 0 { // random.nextBoolean()
		return false
	}
	if e.maxLocalRawBrightness(pos) > e.rnd.IntN(4) {
		return false
	}
	return e.hasTag(pos.Side(cube.FaceDown), "bats_spawnable_on") && checkMobSpawnRules(e, t, pos)
}

// checkEndermiteSpawnRules is Endermite.checkEndermiteSpawnRules and
// Silverfish.checkSilverfishSpawnRules: no survival or adventure player
// within 5 blocks of the block centre.
func checkEndermiteSpawnRules(e *env, t *EntityType, pos cube.Pos) bool {
	if !checkAnyLightMonsterSpawnRules(e, t, pos) {
		return false
	}
	if e.spawner {
		return true
	}
	c := mgl64.Vec3{float64(pos[0]) + .5, float64(pos[1]) + .5, float64(pos[2]) + .5}
	for _, p := range e.st.players {
		if !p.creative && p.pos.Sub(c).LenSqr() < 25 {
			return false
		}
	}
	return true
}

// checkPatrollingMonsterSpawnRules is PatrollingMonster.checkPatrollingMonsterSpawnRules.
func checkPatrollingMonsterSpawnRules(e *env, t *EntityType, pos cube.Pos) bool {
	if e.blockLight(pos) > 8 {
		return false
	}
	return checkAnyLightMonsterSpawnRules(e, t, pos)
}

// checkMagmaCubeSpawnRules is MagmaCube.checkMagmaCubeSpawnRules.
func checkMagmaCubeSpawnRules(e *env, _ *EntityType, _ cube.Pos) bool { return !e.peaceful() }

// notOnNetherWart is Hoglin.checkHoglinSpawnRules and Piglin.checkPiglinSpawnRules.
func notOnNetherWart(e *env, _ *EntityType, pos cube.Pos) bool {
	return javaName(e.tx.Block(pos.Side(cube.FaceDown))) != "nether_wart_block"
}

// checkZombifiedPiglinSpawnRules is ZombifiedPiglin.checkZombifiedPiglinSpawnRules.
func checkZombifiedPiglinSpawnRules(e *env, t *EntityType, pos cube.Pos) bool {
	return !e.peaceful() && notOnNetherWart(e, t, pos)
}

// checkGhastSpawnRules is Ghast.checkGhastSpawnRules.
func checkGhastSpawnRules(e *env, t *EntityType, pos cube.Pos) bool {
	return !e.peaceful() && e.rnd.IntN(20) == 0 && checkMobSpawnRules(e, t, pos)
}

// checkStraySpawnRules is Stray.checkStraySpawnRules: the sky must be
// visible above any powder snow over the stray.
func checkStraySpawnRules(e *env, t *EntityType, pos cube.Pos) bool {
	check := pos.Side(cube.FaceUp)
	for javaName(e.tx.Block(check)) == "powder_snow" {
		check = check.Side(cube.FaceUp)
	}
	return checkMonsterSpawnRules(e, t, pos) && (e.spawner || e.canSeeSky(check.Side(cube.FaceDown)))
}

// checkStriderSpawnRules is Strider.checkStriderSpawnRules: air above the
// lava column.
func checkStriderSpawnRules(e *env, _ *EntityType, pos cube.Pos) bool {
	check := pos.Side(cube.FaceUp)
	for {
		if _, l := fluidAt(e.tx, check); !l {
			break
		}
		check = check.Side(cube.FaceUp)
	}
	return isAir(e.tx, check)
}

// checkGuardianSpawnRules is Guardian.checkGuardianSpawnRules.
func checkGuardianSpawnRules(e *env, _ *EntityType, pos cube.Pos) bool {
	if !(e.rnd.IntN(20) == 0 || !e.canSeeSkyFromBelowWater(pos)) || e.peaceful() {
		return false
	}
	w, _ := fluidAt(e.tx, pos)
	wb, _ := fluidAt(e.tx, pos.Side(cube.FaceDown))
	return (e.spawner || w) && wb
}

// checkDrownedSpawnRules is Drowned.checkDrownedSpawnRules for a natural
// spawn: in water over water, dark, 1 in 15 in rivers (more frequent
// drowned spawns), else 1 in 40 and below sea level - 5.
func checkDrownedSpawnRules(e *env, _ *EntityType, pos cube.Pos) bool {
	if w, _ := fluidAt(e.tx, pos.Side(cube.FaceDown)); !w {
		return false
	}
	frequent := e.biomeAt(pos).tags["more_frequent_drowned_spawns"]
	canMonsterSpawn := !e.peaceful() && e.isDarkEnoughToSpawn(pos)
	if canMonsterSpawn {
		canMonsterSpawn, _ = fluidAt(e.tx, pos)
	}
	if frequent {
		return e.rnd.IntN(15) == 0 && canMonsterSpawn
	}
	return e.rnd.IntN(40) == 0 && pos[1] < e.seaLevel-5 && canMonsterSpawn
}

// checkSlimeSpawnRules is Slime.checkSlimeSpawnRules with Paper's slime
// spawn heights at their defaults (surface biome 50..70, slime chunks below
// 40) and all-chunks-are-slime-chunks.
func checkSlimeSpawnRules(e *env, t *EntityType, pos cube.Pos) bool {
	if e.peaceful() {
		return false
	}
	if e.spawner {
		return checkMobSpawnRules(e, t, pos)
	}
	if e.biomeAt(pos).tags["allows_surface_slime_spawns"] && pos[1] > 50 && pos[1] < 70 {
		if e.rnd.Float32() < e.surfaceSlimeChance && e.maxLocalRawBrightness(pos) <= e.rnd.IntN(8) {
			return checkMobSpawnRules(e, t, pos)
		}
	}
	slimeChunk := e.cfg.AllChunksAreSlimeChunks || IsSlimeChunk(int32(pos[0]>>4), int32(pos[2]>>4), e.cfg.Seed, e.cfg.SlimeSeed)
	if e.rnd.IntN(10) == 0 && slimeChunk && pos[1] < 40 {
		return checkMobSpawnRules(e, t, pos)
	}
	return false
}

// IsSlimeChunk is WorldgenRandom.seedSlimeChunk(x, z, seed, salt).nextInt(10) == 0
// with Java's int arithmetic and java.util.Random.
func IsSlimeChunk(x, z int32, seed, salt int64) bool {
	s := seed + int64(x*x*4987142) + int64(x*5947611) + int64(z*z)*4392871 + int64(z*389711) ^ salt
	return newJavaRandom(s).nextInt(10) == 0
}

// surfaceSlimeSpawnChance is gameplay/surface_slime_spawn_chance of
// timeline/moon.json: 0.5 at full moon down to 0 at new moon.
func surfaceSlimeSpawnChance(timeOfDay int) float32 {
	day := ((timeOfDay % 192000) + 192000) % 192000 / 24000
	return [8]float32{.5, .375, .25, .125, 0, .125, .25, .375}[day]
}

// walkTargetKind is the getWalkTargetValue override of a mob class, read by
// PathfinderMob.checkSpawnRules (walk target value >= 0).
type walkTargetKind uint8

const (
	walkZero    walkTargetKind = iota // PathfinderMob (0) or not a PathfinderMob (Mob.checkSpawnRules: true)
	walkMonster                       // Monster: -cost
	walkAnimal                        // Animal: grass block below: 10, else cost
	walkMooshroom
	walkTurtle
	walkSilverfish
	walkGuardian
	walkGiant
)

var walkTargets = map[string]walkTargetKind{
	"minecraft:mooshroom": walkMooshroom, "minecraft:turtle": walkTurtle, "minecraft:silverfish": walkSilverfish,
	"minecraft:guardian": walkGuardian, "minecraft:elder_guardian": walkGuardian, "minecraft:giant": walkGiant,
	// Enderman, Creaking, Pillager and Warden return 0; Bee, HappyGhast,
	// Hoglin, Strider, Axolotl and the nautili never go below 0.
}

// monsterClasses are the 26.3 types that extend Monster without a
// getWalkTargetValue override; animalClasses those that extend Animal
// without one.
var (
	monsterClasses = []string{"blaze", "bogged", "breeze", "cave_spider", "creeper", "drowned", "endermite", "evoker",
		"husk", "illusioner", "parched", "piglin", "piglin_brute", "ravager", "skeleton", "spider", "stray", "vex",
		"vindicator", "witch", "wither", "wither_skeleton", "zoglin", "zombie", "zombie_villager", "zombified_piglin"}
	animalClasses = []string{"armadillo", "camel", "camel_husk", "cat", "chicken", "cow", "donkey", "fox", "frog",
		"goat", "horse", "llama", "mule", "ocelot", "panda", "parrot", "pig", "polar_bear", "rabbit", "sheep",
		"skeleton_horse", "sniffer", "trader_llama", "wolf", "zombie_horse"}
)

func init() {
	for _, n := range monsterClasses {
		walkTargets["minecraft:"+n] = walkMonster
	}
	for _, n := range animalClasses {
		walkTargets["minecraft:"+n] = walkAnimal
	}
}

// mobCheckSpawnRules is Mob.checkSpawnRules of a fresh mob at pos
// (PathfinderMob: getWalkTargetValue(blockPosition) >= 0).
func (e *env) mobCheckSpawnRules(t *EntityType, pos cube.Pos) bool {
	below := pos.Side(cube.FaceDown)
	switch walkTargets[t.ID] {
	case walkMonster:
		return -e.pathfindingCostFromLightLevels(pos) >= 0
	case walkAnimal:
		return javaName(e.tx.Block(below)) == "grass_block" || e.pathfindingCostFromLightLevels(pos) >= 0
	case walkMooshroom:
		return javaName(e.tx.Block(below)) == "mycelium" || e.pathfindingCostFromLightLevels(pos) >= 0
	case walkTurtle:
		if w, _ := fluidAt(e.tx, pos); w || e.hasTag(below, "sand") {
			return true
		}
		return e.pathfindingCostFromLightLevels(pos) >= 0
	case walkSilverfish:
		switch javaName(e.tx.Block(below)) {
		case "stone", "cobblestone", "stone_bricks", "mossy_stone_bricks", "cracked_stone_bricks",
			"chiseled_stone_bricks", "deepslate": // InfestedBlock.isCompatibleHostBlock
			return true
		}
		return -e.pathfindingCostFromLightLevels(pos) >= 0
	case walkGuardian:
		if w, _ := fluidAt(e.tx, pos); w {
			return 10+e.pathfindingCostFromLightLevels(pos) >= 0
		}
		return -e.pathfindingCostFromLightLevels(pos) >= 0
	case walkGiant:
		return e.pathfindingCostFromLightLevels(pos) >= 0
	}
	return true
}

// obstructionKind is the checkSpawnObstruction override of a mob class.
type obstructionKind uint8

const (
	obstructionDefault    obstructionKind = iota // no liquid in the box and unobstructed
	obstructionUnobstruct                        // water and lava mobs: only unobstructed
	obstructionRavager                           // no liquid only
	obstructionOcelot
	obstructionWarden
)

var obstructions = map[string]obstructionKind{
	"minecraft:axolotl": obstructionUnobstruct, "minecraft:nautilus": obstructionUnobstruct,
	"minecraft:zombie_nautilus": obstructionUnobstruct, "minecraft:squid": obstructionUnobstruct,
	"minecraft:glow_squid": obstructionUnobstruct, "minecraft:dolphin": obstructionUnobstruct,
	"minecraft:cod": obstructionUnobstruct, "minecraft:salmon": obstructionUnobstruct,
	"minecraft:pufferfish": obstructionUnobstruct, "minecraft:tropical_fish": obstructionUnobstruct,
	"minecraft:guardian": obstructionUnobstruct, "minecraft:elder_guardian": obstructionUnobstruct,
	"minecraft:drowned": obstructionUnobstruct, "minecraft:strider": obstructionUnobstruct,
	"minecraft:ravager": obstructionRavager, "minecraft:ocelot": obstructionOcelot, "minecraft:warden": obstructionWarden,
}

// checkSpawnObstruction is Mob.checkSpawnObstruction of a fresh mob whose
// bounding box is box.
func (e *env) checkSpawnObstruction(t *EntityType, pos cube.Pos, box cube.BBox) bool {
	switch obstructions[t.ID] {
	case obstructionUnobstruct:
		return e.isUnobstructed(box)
	case obstructionRavager:
		return !e.containsAnyLiquid(box)
	case obstructionOcelot:
		if !e.isUnobstructed(box) || e.containsAnyLiquid(box) || pos[1] < e.seaLevel {
			return false
		}
		below := infoOf(e.tx.Block(pos.Side(cube.FaceDown)))
		return below.name == "grass_block" || below.leaves
	case obstructionWarden:
		return !e.containsAnyLiquid(box) && e.isUnobstructed(box) && e.noBlockCollision(box)
	}
	return !e.containsAnyLiquid(box) && e.isUnobstructed(box)
}

// containsAnyLiquid is BlockGetter.containsAnyLiquid.
func (e *env) containsAnyLiquid(box cube.BBox) bool {
	mn, mx := box.Min(), box.Max()
	for x := floor(mn[0]); x < ceil(mx[0]); x++ {
		for y := floor(mn[1]); y < ceil(mx[1]); y++ {
			for z := floor(mn[2]); z < ceil(mx[2]); z++ {
				if w, l := fluidAt(e.tx, cube.Pos{x, y, z}); w || l {
					return true
				}
			}
		}
	}
	return false
}

// isUnobstructed is LevelReader.isUnobstructed(entity): no living entity
// (players, mobs: Entity.blocksBuilding) collides with the box.
func (e *env) isUnobstructed(box cube.BBox) bool {
	for other := range e.tx.EntitiesWithin(box.Grow(1)) {
		if !blocksBuilding(other) {
			continue
		}
		if other.H().Type().BBox(other).Translate(other.Position()).IntersectsWith(box) {
			return false
		}
	}
	return true
}

func blocksBuilding(e world.Entity) bool {
	switch v := e.(type) {
	case *player.Player:
		return v.GameMode() != world.GameModeSpectator
	case entity.Living:
		return !v.Dead()
	}
	if reg := currentRegistry(); reg != nil {
		_, ok := reg.Mob(e)
		return ok
	}
	return false
}

// noBlockCollision is CollisionGetter.noCollision for the block shapes.
func (e *env) noBlockCollision(box cube.BBox) bool {
	for _, bb := range entity.BlockBBoxesAround(e.tx, box) {
		if bb.IntersectsWith(box) {
			return false
		}
	}
	return true
}

// spawnAABB is EntityType.getSpawnAABB(x, y, z).
func spawnAABB(t *EntityType, x, y, z float64) cube.BBox {
	hw := t.Width / 2
	return cube.Box(x-hw, y, z-hw, x+hw, y+t.Height, z+hw)
}

// maxSpawnClusterSize is Mob.getMaxSpawnClusterSize.
func maxSpawnClusterSize(typ string) int {
	switch typ {
	case "minecraft:horse", "minecraft:donkey", "minecraft:mule", "minecraft:skeleton_horse", "minecraft:zombie_horse",
		"minecraft:llama", "minecraft:trader_llama", "minecraft:camel", "minecraft:camel_husk":
		return 6 // AbstractHorse
	case "minecraft:cod", "minecraft:pufferfish", "minecraft:tropical_fish", "minecraft:wolf":
		return 8 // AbstractFish, AbstractSchoolingFish.getMaxSchoolSize, Wolf
	case "minecraft:salmon":
		return 5 // Salmon.getMaxSchoolSize
	case "minecraft:ghast", "minecraft:happy_ghast", "minecraft:pillager":
		return 1
	}
	return 4
}

// removeWhenFarAway returns Mob.removeWhenFarAway of m, or the vanilla
// default of its type for a mob that does not implement it. fresh is true
// for a mob that was just created (tickCount 0).
func removeWhenFarAway(m any, typ string, distSqr float64, fresh bool) bool {
	if f, ok := m.(farAwayMob); ok {
		return f.RemoveWhenFarAway(distSqr)
	}
	return defaultRemoveWhenFarAway(typ, fresh)
}

// defaultRemoveWhenFarAway is removeWhenFarAway of an unnamed mob in its
// natural state: Animal false (but hoglins, zombie horses, camel husks and
// chicken jockeys), golems, villagers, allays and wardens false, untamed
// cats and ocelots after 2400 ticks, everything else true (fish and
// axolotls not from a bucket, patrol mobs not patrolling, zombie villagers
// not converting).
func defaultRemoveWhenFarAway(typ string, fresh bool) bool {
	switch typ {
	case "minecraft:cat", "minecraft:ocelot":
		return !fresh
	case "minecraft:hoglin", "minecraft:zombie_horse", "minecraft:camel_husk", "minecraft:axolotl",
		"minecraft:nautilus", "minecraft:zombie_nautilus":
		return true
	case "minecraft:villager", "minecraft:wandering_trader", "minecraft:iron_golem", "minecraft:snow_golem",
		"minecraft:shulker", "minecraft:copper_golem", "minecraft:allay", "minecraft:warden":
		return false
	}
	return walkTargets[typ] != walkAnimal && typ != "minecraft:mooshroom" && typ != "minecraft:turtle" &&
		typ != "minecraft:bee" && typ != "minecraft:strider" && typ != "minecraft:sniffer" &&
		typ != "minecraft:happy_ghast" && typ != "minecraft:goat"
}
