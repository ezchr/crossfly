//go:build crossfly_full

package jfarm

import (
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

func trimNS(s string) string { return strings.TrimPrefix(s, "minecraft:") }

// pick is Registry.getRandom over the sound variant entries (uniform).
func pick(m *javamob.Mob, names ...string) string { return names[m.Rand().IntN(len(names))] }

// bedrockSound is the minecraft:sound_variant value Bedrock uses for a Java sound variant.
func bedrockSound(s string) string {
	if s == "classic" {
		return "default"
	}
	return s
}

// javaItem is the item registered under the Java name n.
func javaItem(n string) (world.Item, bool) {
	if n == "flowering_azalea_leaves" {
		n = "azalea_leaves_flowered"
	}
	return world.ItemByName("minecraft:"+n, 0)
}

func playSound(m *javamob.Mob, name string) { playSoundVol(m, name, 1) }

func playSoundVol(m *javamob.Mob, name string, vol float64) {
	m.Tx().PlaySound(m.Position(), sound.Custom{Name: name, Volume: vol, Pitch: 1})
}

// gameEvent is Entity.gameEvent: Crossfly's game events are emitted by the world for block changes; mob
// actions have no listener yet.
func gameEvent(*javamob.Mob, string) {}

// spawnItemWithPush is Entity.spawnAtLocation(stack, yOffset) with an extra push on the item's motion.
func spawnItemWithPush(m *javamob.Mob, s item.Stack, yOff float64, push mgl64.Vec3) {
	r := m.Rand()
	at := m.Position().Add(mgl64.Vec3{0, yOff, 0})
	v := mgl64.Vec3{r.Float64()*0.2 - 0.1, 0.2, r.Float64()*0.2 - 0.1}.Add(push)
	opts := world.EntitySpawnOpts{Position: at, Velocity: v}
	m.Tx().AddEntity(m.Tx().World().EntityRegistry().Config().Item(opts, s))
}

func dropAt(tx *world.Tx, at mgl64.Vec3, s item.Stack) {
	opts := world.EntitySpawnOpts{Position: at}
	tx.AddEntity(tx.World().EntityRegistry().Config().Item(opts, s))
}

func sneaking(user item.User) bool {
	s, ok := user.(interface{ Sneaking() bool })
	return ok && s.Sneaking()
}

// mount is Player.startRiding(mob).
func mount(m *javamob.Mob, user item.User) bool {
	r, ok := user.(interface{ RideEntity(e world.Entity) bool })
	return ok && r.RideEntity(m.Mob)
}

func smokeParticles(m *javamob.Mob) {
	m.Tx().AddParticle(m.Position().Add(mgl64.Vec3{0, m.Height() / 2, 0}), particle.Named{Bedrock: "minecraft:basic_smoke_particle", Java: "smoke"})
}

func explosionParticle(m *javamob.Mob) {
	m.Tx().AddParticle(m.Position().Add(mgl64.Vec3{0, m.Height() / 2, 0}), particle.Named{Bedrock: "minecraft:large_explosion", Java: "explosion"})
}

func convertTo(m *javamob.Mob, id string) bool {
	_, ok := m.ConvertTo(id, false, nil)
	return ok
}

// suspiciousStew is the stew a brown mooshroom fed javaFlower gives.
func suspiciousStew(javaFlower string) item.Stack {
	t, ok := stewTypes[javaFlower]
	if !ok {
		return item.NewStack(item.MushroomStew{}, 1)
	}
	return item.NewStack(item.SuspiciousStew{Type: t()}, 1)
}

var stewTypes = map[string]func() item.StewType{
	"poppy": item.NightVisionPoppyStew, "torchflower": item.NightVisionPoppyStew, "cornflower": item.JumpBoostStew,
	"red_tulip": item.WeaknessStew, "orange_tulip": item.WeaknessStew, "white_tulip": item.WeaknessStew,
	"pink_tulip": item.WeaknessStew, "azure_bluet": item.BlindnessBluetStew, "open_eyeblossom": item.BlindnessBluetStew,
	"lily_of_the_valley": item.PoisonStew, "dandelion": item.SaturationDandelionStew,
	"golden_dandelion": item.SaturationDandelionStew, "blue_orchid": item.SaturationOrchidStew,
	"allium": item.FireResistanceStew, "oxeye_daisy": item.RegenerationStew, "wither_rose": item.WitherStew,
}

// blockPos is Entity.blockPosition.
func blockPos(m *javamob.Mob) cube.Pos { return cube.PosFromVec3(m.Position()) }
