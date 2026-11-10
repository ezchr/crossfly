//go:build crossfly_full

package misc

import (
	"hash/fnv"
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// Goat horns (BDS class Goat, RamAttackGoalUtils::RamGoalItemDropperImpl).
// The horn count is actor data 122 (GOAT_HORN_COUNT, an int), from which
// the Bedrock client draws the left (count > 0) and right (count > 1) horn;
// it is saved as GoatHornCount.
const (
	goatID           = "minecraft:goat"
	goatHornsDefault = 2 // Goat::DEFAULT_HORN_COUNT
	goatScreaming    = 1 // Goat::SCREAMING_GOAT_VARIANT
)

func init() {
	mob.RegisterSystem(mob.System{Name: "misc-goat/horns", Stage: mob.StageSensors, Order: 240, Tick: goatHornsTick})
	mob.RegisterNBTHook(mob.NBTHook{Name: "misc-goat/horns", Encode: encodeGoatHorns, Decode: decodeGoatHorns})
}

// GoatHornCount returns the horns a goat has left (2 by default).
func GoatHornCount(m *mob.Mob) int {
	if v, ok := m.ActorData(protocol.EntityDataKeyGoatHornCount); ok {
		return int(v.(int32))
	}
	return goatHornsDefault
}

func setGoatHorns(m *mob.Mob, n int) {
	m.SetActorData(protocol.EntityDataKeyGoatHornCount, int32(n))
}

// goatRand is a generator seeded from the goat's identity, for the rolls
// BDS makes with a Random seeded from the goat's unique id: they come out
// the same every time for one goat.
func goatRand(m *mob.Mob, salt string) *rand.Rand {
	h := fnv.New64a()
	id := m.H().UUID()
	_, _ = h.Write(id[:])
	_, _ = h.Write([]byte(salt))
	s := h.Sum64()
	return rand.New(rand.NewPCG(s, s^0x9e3779b97f4a7c15))
}

// goatOneHorn is the roll of Goat::initializeComponents: 1 goat in 10 has
// one horn.
func goatOneHorn(m *mob.Mob) bool { return goatRand(m, "horns").Uint32()%10 == 0 }

type goatHornsKey struct{}

// goatHornsState remembers whether the goat was a baby, to run
// Goat::initializeComponents again when that changes.
type goatHornsState struct{ baby bool }

// goatHornsTick is Goat::initializeComponents, which BDS runs when the goat
// spawns and whenever its components change: a baby has no horns; an adult
// gets one horn 1 time in 10 (else it keeps the count it has, two for a new
// goat). So a kid that grows up has no horns unless it rolls the one.
// Loaded goats keep their saved count.
func goatHornsTick(m *mob.Mob) {
	if m.Identifier() != goatID {
		return
	}
	baby := m.Baby()
	v, ok := m.Data(goatHornsKey{})
	if !ok {
		m.SetData(goatHornsKey{}, &goatHornsState{baby: baby})
		if _, saved := m.ActorData(protocol.EntityDataKeyGoatHornCount); saved && !baby {
			return
		}
		goatInitHorns(m, baby)
		return
	}
	st := v.(*goatHornsState)
	if st.baby != baby {
		st.baby = baby
		goatInitHorns(m, baby)
	}
}

func goatInitHorns(m *mob.Mob, baby bool) {
	switch {
	case baby:
		setGoatHorns(m, 0)
	case goatOneHorn(m):
		setGoatHorns(m, 1)
	default:
		setGoatHorns(m, GoatHornCount(m))
	}
}

// encodeGoatHorns is Goat::addAdditionalSaveData.
func encodeGoatHorns(m *mob.Mob) map[string]any {
	if m.Identifier() != goatID {
		return nil
	}
	return map[string]any{"GoatHornCount": int32(GoatHornCount(m))}
}

// decodeGoatHorns is Goat::readAdditionalSaveData.
func decodeGoatHorns(m *mob.Mob, d map[string]any) {
	if n, ok := d["GoatHornCount"].(int32); ok && m.Identifier() == goatID {
		setGoatHorns(m, int(n))
	}
}

// goatHornBlocks are the blocks a ramming goat loses a horn on
// (RamGoalItemDropperImpl::checkForHornDropOnCollision): the logs
// (VanillaBlockTypeGroups::LogBlockIds, not stems, wood or stripped logs),
// stone, packed ice and the coal, copper, iron and emerald ores (not their
// deepslate kinds).
var goatHornBlocks = map[string]bool{
	"minecraft:oak_log": true, "minecraft:spruce_log": true, "minecraft:birch_log": true,
	"minecraft:jungle_log": true, "minecraft:acacia_log": true, "minecraft:dark_oak_log": true,
	"minecraft:mangrove_log": true, "minecraft:cherry_log": true, "minecraft:pale_oak_log": true,
	"minecraft:stone": true, "minecraft:packed_ice": true, "minecraft:coal_ore": true,
	"minecraft:copper_ore": true, "minecraft:iron_ore": true, "minecraft:emerald_ore": true,
}

// goatHornBlockAhead is RamAttackGoal::tick starting the charge with
// checkForHornDropOnCollision: the block one past the ram's end, along the
// axis the goat mostly charges on, is a horn block.
func goatHornBlockAhead(tx *world.Tx, from, end mgl64.Vec3) bool {
	dx := int(math.Round(from[0] - end[0]))
	dz := int(math.Round(from[2] - end[2]))
	p := end
	if absInt(dx) <= absInt(dz) {
		if dz >= 0 {
			p[2]--
		} else {
			p[2]++
		}
	} else if dx >= 0 {
		p[0]--
	} else {
		p[0]++
	}
	n, _ := tx.Block(cube.PosFromVec3(p)).EncodeBlock()
	return goatHornBlocks[n]
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// goatDropHorn is RamGoalItemDropperImpl::tryDropHorn after a ram that ended
// against a horn block: a goat with horns left loses one (Goat::
// reduceHornCount), a goat horn drops at the end of the ram with a half
// second pickup delay, and the horn_break sound plays at the goat. The horn
// is one of the four regular ones, or of the four screaming ones for a
// screaming goat, always the same for one goat
// (GoatHornItem::addGoatHornVarationSaveData).
func goatDropHorn(m *mob.Mob, at mgl64.Vec3) {
	if m.Identifier() != goatID {
		return
	}
	n := GoatHornCount(m)
	if n <= 0 {
		return
	}
	setGoatHorns(m, n-1)
	v := int(goatRand(m, "horn variant").Uint32() & 3)
	if m.Variant() == goatScreaming {
		v += 4
	}
	horn := item.NewStack(item.GoatHorn{Type: sound.GoatHorns()[v]}, 1)
	m.Tx().AddEntity(entity.NewItemPickupDelay(world.EntitySpawnOpts{Position: at}, horn, time.Second/2))
	m.PlaySound("horn_break")
}
