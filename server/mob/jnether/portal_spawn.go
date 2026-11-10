//go:build crossfly_full

package jnether

import (
	"math/rand/v2"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Zombified piglins from nether portals (NetherPortalBlock.randomTick, with
// Paper's defaults: Spigot enable-zombie-pigmen-portal-spawns true, Paper
// nerf-pigmen-from-nether-portals false). Every random tick of a portal
// block in the Overworld (the dimension's nether_portal_spawns_piglin
// attribute), when monsters spawn and the difficulty is not peaceful, has a
// chance of difficulty/2000 (1 easy, 2 normal, 3 hard) to spawn a
// zombified piglin on the first block under the portal column that is a
// valid spawn for it, provided a player is within 128 blocks of the chunk.
// The piglin and its vehicle get the portal cooldown (300 ticks), so they do
// not walk straight back through the portal: gold farms rely on that.

// PortalCooldown is Entity.getDimensionChangingDelay for non-players: 300
// ticks.
const PortalCooldown = 300 * time.Second / 20

// portalSpawnChance is the "random.nextInt(2000) < difficulty" roll.
const portalSpawnChance = 2000

// PortalSpawnHooks are what the portal spawning needs from the rest of Java
// mob mode. Install sets them.
type PortalSpawnHooks struct {
	// SpawnZombifiedPiglin spawns a zombified piglin with
	// EntitySpawnReason.STRUCTURE at pos (the block's bottom centre) facing
	// yaw (EntityType.spawn: finalizeSpawn runs, so equipment and babies
	// are rolled) and returns it, or nil.
	SpawnZombifiedPiglin func(tx *world.Tx, pos mgl64.Vec3, yaw float64) world.Entity
	// SpawningMonsters is ServerLevel.isSpawningMonsters (the spawn_monsters
	// game rule); nil means true.
	SpawningMonsters func(tx *world.Tx) bool
	// ValidSpawn is BlockState.isValidSpawn for a zombified piglin; nil
	// uses validSpawnZombifiedPiglin.
	ValidSpawn func(tx *world.Tx, pos cube.Pos) bool
	// PlayerNear is ServerLevel.anyPlayerCloseEnoughForSpawning; nil uses
	// anyPlayerCloseEnoughForSpawning.
	PlayerNear func(tx *world.Tx, pos cube.Pos) bool
}

var portalHooks PortalSpawnHooks

// InstallPortalSpawning makes nether portal blocks spawn zombified piglins
// (block.PortalRandomTick). Call it once before any world ticks.
func InstallPortalSpawning(h PortalSpawnHooks) {
	portalHooks = h
	block.PortalRandomTick = portalRandomTick
}

// portalRandomTick is NetherPortalBlock.randomTick.
func portalRandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	h := portalHooks
	if h.SpawnZombifiedPiglin == nil {
		return
	}
	if h.SpawningMonsters != nil && !h.SpawningMonsters(tx) {
		return
	}
	diff := difficultyID(tx)
	if diff == 0 || tx.World().Dimension() != world.Overworld {
		return
	}
	if r.IntN(portalSpawnChance) >= diff {
		return
	}
	near := anyPlayerCloseEnoughForSpawning
	if h.PlayerNear != nil {
		near = h.PlayerNear
	}
	if !near(tx, pos) {
		return
	}
	for pos[1] >= tx.Range().Min() {
		if _, ok := tx.Block(pos).(block.Portal); !ok {
			break
		}
		pos = pos.Side(cube.FaceDown)
	}
	valid := validSpawnZombifiedPiglin
	if h.ValidSpawn != nil {
		valid = h.ValidSpawn
	}
	if !valid(tx, pos) {
		return
	}
	at := pos.Side(cube.FaceUp)
	// EntityType.create: snapTo(x + 0.5, y, z + 0.5, random yaw).
	e := h.SpawnZombifiedPiglin(tx, mgl64.Vec3{float64(at[0]) + 0.5, float64(at[1]), float64(at[2]) + 0.5}, r.Float64()*360)
	if e == nil {
		return
	}
	SetPortalCooldown(e)
	if rd, ok := e.(interface{ Vehicle() world.Entity }); ok {
		if v := rd.Vehicle(); v != nil {
			SetPortalCooldown(v)
		}
	}
}

// SetPortalCooldown is Entity.setPortalCooldown: the entity may not travel
// through a portal for 300 ticks.
func SetPortalCooldown(e world.Entity) {
	var b any = e
	if ent, ok := e.(interface{ Behaviour() entity.Behaviour }); ok {
		b = ent.Behaviour()
	}
	if p, ok := b.(interface {
		PortalTravelComputer() *entity.PortalTravelComputer
	}); ok {
		p.PortalTravelComputer().SetCooldown(PortalCooldown)
		return
	}
	if p, ok := e.(interface {
		PortalTravelComputer() *entity.PortalTravelComputer
	}); ok {
		p.PortalTravelComputer().SetCooldown(PortalCooldown)
	}
}

// anyPlayerCloseEnoughForSpawning is ChunkMap.anyPlayerCloseEnoughForSpawning
// (Paper, not reduced): a player who is not a spectator within 128 blocks,
// horizontally, of the middle of the chunk.
func anyPlayerCloseEnoughForSpawning(tx *world.Tx, pos cube.Pos) bool {
	cx, cz := float64(pos[0]>>4<<4+8), float64(pos[2]>>4<<4+8)
	for p := range tx.Players() {
		if g, ok := p.(interface{ GameMode() world.GameMode }); ok && !g.GameMode().Visible() {
			continue
		}
		pp := p.Position()
		dx, dz := cx-pp[0], cz-pp[2]
		if dx*dx+dz*dz < 16384 {
			return true
		}
	}
	return false
}

// validSpawnZombifiedPiglin is BlockState.isValidSpawn(level, pos,
// ZOMBIFIED_PIGLIN): by default a full top face and a light emission below
// 14 (BlockBehaviour.Properties.isValidSpawn), with the Blocks overrides:
// never on glass, leaves (ocelots and parrots only), ice (polar bears),
// trapdoors, barriers, scaffolding, chorus flowers, repeaters; always on
// soul sand, pumpkins, redstone lamps and mud; magma blocks for fire-immune
// mobs, which zombified piglins are.
func validSpawnZombifiedPiglin(tx *world.Tx, pos cube.Pos) bool {
	b := tx.Block(pos)
	name, _ := b.EncodeBlock()
	name = strings.TrimPrefix(name, "minecraft:")
	switch {
	case name == "soul_sand" || name == "carved_pumpkin" || name == "lit_pumpkin" || name == "redstone_lamp" ||
		name == "lit_redstone_lamp" || name == "mud" || name == "magma":
		return true
	case strings.HasSuffix(name, "glass") || strings.HasSuffix(name, "leaves") || name == "ice" || name == "frosted_ice" ||
		strings.HasSuffix(name, "trapdoor") || name == "barrier" || name == "scaffolding" || name == "chorus_flower" ||
		name == "unpowered_repeater" || name == "powered_repeater" || name == "moving_block" || name == "bedrock":
		return false
	}
	if l, ok := b.(interface{ LightEmissionLevel() uint8 }); ok && l.LightEmissionLevel() >= 14 {
		return false
	}
	return b.Model().FaceSolid(pos, cube.FaceUp, tx)
}
