//go:build crossfly_full

package social

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/component/tame"
)

// The parrot's hard-coded parts (BDS Parrot) besides dancing (dance.go): imitating monsters
// (Parrot::playAmbientSound, Parrot::getAmbientSound, ImitateMobSoundsComponent), no damage while
// riding (Parrot::isInvulnerableTo) and no sitting off the ground (Parrot::postAiStep).

func init() {
	mob.RegisterAmbientOverride(parrotAmbient)
	mob.RegisterDamageHook(mob.DamageHook{Name: "social/parrot_riding", Modify: parrotInvulnerable})
	mob.RegisterSystem(mob.System{Name: "social/parrot", Stage: mob.StageBody, Tick: parrotTick})
}

const parrotID = "minecraft:parrot"

// imitateSounds is ImitateMobSoundsComponent::imitateMobMap: the sound a parrot imitates of each
// entity type (BDS 1.21.3, plus the entries the 26.60 server added: creaking, drowned, guardian,
// happy ghast, parched, phantom, pillager, ravager, zoglin and camel husk). Wolves, polar bears,
// happy ghasts and camel husks are no monsters, so only Parrot::getAmbientSound's random pick
// plays theirs.
var imitateSounds = map[string]string{
	"minecraft:blaze": "imitate.blaze", "minecraft:bogged": "imitate.bogged", "minecraft:breeze": "imitate.breeze",
	"minecraft:cave_spider": "imitate.cave_spider", "minecraft:creeper": "imitate.creeper",
	"minecraft:elder_guardian": "imitate.elder_guardian", "minecraft:ender_dragon": "imitate.ender_dragon",
	"minecraft:enderman": "imitate.enderman", "minecraft:endermite": "imitate.endermite",
	"minecraft:evocation_illager": "imitate.evocation_illager", "minecraft:ghast": "imitate.ghast",
	"minecraft:husk": "imitate.husk", "minecraft:magma_cube": "imitate.magma_cube",
	"minecraft:zombie_pigman": "imitate.zombie_pigman", "minecraft:polar_bear": "imitate.polar_bear",
	"minecraft:shulker": "imitate.shulker", "minecraft:silverfish": "imitate.silverfish",
	"minecraft:skeleton": "imitate.skeleton", "minecraft:slime": "imitate.slime", "minecraft:spider": "imitate.spider",
	"minecraft:stray": "imitate.stray", "minecraft:vex": "imitate.vex", "minecraft:vindicator": "imitate.vindication_illager",
	"minecraft:witch": "imitate.witch", "minecraft:wither": "imitate.wither",
	"minecraft:wither_skeleton": "imitate.wither_skeleton", "minecraft:wolf": "imitate.wolf",
	"minecraft:zombie": "imitate.zombie", "minecraft:zombie_villager": "imitate.zombie_villager",
	"minecraft:zombie_villager_v2": "imitate.zombie_villager", "minecraft:warden": "imitate.warden",
	// 26.60.
	"minecraft:creaking": "imitate.creaking", "minecraft:drowned": "imitate.drowned",
	"minecraft:guardian": "imitate.guardian", "minecraft:happy_ghast": "imitate.happy_ghast",
	"minecraft:parched": "imitate.parched", "minecraft:phantom": "imitate.phantom",
	"minecraft:pillager": "imitate.pillager", "minecraft:ravager": "imitate.ravager",
	"minecraft:zoglin": "imitate.zoglin", "minecraft:camel_husk": "imitate.camel_husk",
}

// imitateList is imitateSounds in a fixed order, for the random pick.
var imitateList = []string{
	"imitate.blaze", "imitate.bogged", "imitate.breeze", "imitate.cave_spider", "imitate.creeper",
	"imitate.elder_guardian", "imitate.ender_dragon", "imitate.enderman", "imitate.endermite",
	"imitate.evocation_illager", "imitate.ghast", "imitate.husk", "imitate.magma_cube",
	"imitate.zombie_pigman", "imitate.polar_bear", "imitate.shulker", "imitate.silverfish",
	"imitate.skeleton", "imitate.slime", "imitate.spider", "imitate.stray", "imitate.vex",
	"imitate.vindication_illager", "imitate.witch", "imitate.wither", "imitate.wither_skeleton",
	"imitate.wolf", "imitate.zombie", "imitate.zombie_villager", "imitate.zombie_villager",
	"imitate.warden", "imitate.creaking", "imitate.drowned", "imitate.guardian",
	"imitate.happy_ghast", "imitate.parched", "imitate.phantom", "imitate.pillager",
	"imitate.ravager", "imitate.zoglin", "imitate.camel_husk",
}

// monsters are the entity types of the ActorType Monster class (the 0xb00 bits, the type
// imitateNearbyMobs queries) in BDS 1.21.3's actor type map, plus creaking and parched (26.60).
// The pick is among all of them, so one without an imitation (a drowned in 1.21.3, a skeleton
// horse) means no imitation.
var monsters = map[string]bool{
	"minecraft:blaze": true, "minecraft:bogged": true, "minecraft:breeze": true, "minecraft:cave_spider": true,
	"minecraft:creeper": true, "minecraft:drowned": true, "minecraft:elder_guardian": true,
	"minecraft:ender_dragon": true, "minecraft:enderman": true, "minecraft:endermite": true,
	"minecraft:evocation_illager": true, "minecraft:ghast": true, "minecraft:guardian": true,
	"minecraft:husk": true, "minecraft:magma_cube": true, "minecraft:phantom": true, "minecraft:pillager": true,
	"minecraft:ravager": true, "minecraft:shulker": true, "minecraft:silverfish": true, "minecraft:skeleton": true,
	"minecraft:skeleton_horse": true, "minecraft:slime": true, "minecraft:spider": true, "minecraft:stray": true,
	"minecraft:vex": true, "minecraft:vindicator": true, "minecraft:warden": true, "minecraft:witch": true,
	"minecraft:wither": true, "minecraft:wither_skeleton": true, "minecraft:zoglin": true, "minecraft:zombie": true,
	"minecraft:zombie_horse": true, "minecraft:zombie_pigman": true, "minecraft:zombie_villager": true,
	"minecraft:zombie_villager_v2": true, "minecraft:creaking": true, "minecraft:parched": true,
}

// parrotRand is the random source of the imitations: shared, as they are purely cosmetic and must
// not shift the parrot's own rolls (m.Rand). Tests replace it.
var parrotRand = rand.IntN

// parrotAmbient picks the ambient sound of a parrot for Java viewers (Bedrock clients pick it
// themselves: Parrot::reloadHardcodedClient gives them the ImitateMobSoundsComponent):
// Parrot::playAmbientSound first tries ImitateMobSoundsComponent::imitateNearbyMobs, else
// Actor::playAmbientSound plays Parrot::getAmbientSound, one time in 1000 a random imitation.
func parrotAmbient(m *mob.Mob, event string) (mob.Sound, bool) {
	if m.Identifier() != parrotID {
		return mob.Sound{}, false
	}
	ev, ok := imitateNearbyMobs(m)
	if !ok && parrotRand(1000) == 0 {
		ev, ok = imitateList[parrotRand(len(imitateList))], true // getRandomImitatedSound
	}
	if !ok {
		return mob.Sound{}, false
	}
	return core.MobSound{Event: ev, Identifier: parrotID, Baby: core.IsBaby(m), ClientSide: true}, true
}

// imitateNearbyMobs is ImitateMobSoundsComponent::imitateNearbyMobs: one time in 50, a random
// monster within 20 blocks of the parrot's box; its imitation, if it has one.
func imitateNearbyMobs(m *mob.Mob) (string, bool) {
	if parrotRand(50) != 0 {
		return "", false
	}
	b := m.BBox().Translate(m.Position()).Grow(20)
	var near []string
	for e := range m.Tx().EntitiesWithin(b) {
		if o, ok := mob.Of(e); ok && monsters[o.Identifier()] {
			near = append(near, o.Identifier())
		}
	}
	if len(near) == 0 {
		return "", false
	}
	ev, ok := imitateSounds[near[parrotRand(len(near))]]
	return ev, ok
}

// parrotInvulnerable is Parrot::isInvulnerableTo: a riding parrot (on a shoulder) takes no damage
// but the void's.
func parrotInvulnerable(m *mob.Mob, d *mob.Damage) {
	if m.Identifier() == parrotID && m.Riding() && core.CauseOf(d.Source) != "void" {
		d.Cancel, d.NoHurtAnimation = true, true
	}
}

// parrotTick is the sitting part of Parrot::postAiStep: a riding parrot or one off the ground
// stops sitting (Parrot::setSitting(false); setSitting(true) is refused off the ground the same
// way).
func parrotTick(m *mob.Mob) {
	if m.Identifier() != parrotID || !m.Sitting() || (m.OnGround() && !m.Riding()) {
		return
	}
	if s, ok := tame.SittableOf(m); ok {
		s.SetSitting(m, false)
		return
	}
	m.UpdateMeta(func(meta *mob.Meta) { meta.Sitting = false })
}
