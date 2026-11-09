//go:build crossfly_full

package mob

import (
	"math"
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// EggSpawner spawns the mobs of spawn eggs (item.SpawnEgg). Pass it to item.RegisterSpawnEggs.
type EggSpawner struct{}

// Compile time check that EggSpawner implements item.MobSpawner.
var _ item.MobSpawner = EggSpawner{}

// SpawnFromEgg spawns a mob as BDS's ActorPlacerItem::_spawnActorAt: turned towards the user, fired
// entity_spawned, and if it does not fit where it spawned (a big mob against a wall), moved a block at a
// time towards the user until it does, at most as far as the user is. The mob never despawns.
func (EggSpawner) SpawnFromEgg(tx *world.Tx, id string, pos, towards mgl64.Vec3, nameTag string) bool {
	if _, ok := TypeFor(id); !ok {
		return false
	}
	d := towards.Sub(pos)
	var yaw float64
	if flat := math.Hypot(d[0], d[2]); flat > 1e-4 {
		yaw = math.Atan2(d[2], d[0])*180/math.Pi - 90
	}
	m, err := Spawn(tx, id, pos, SpawnOptions{Rotation: cube.Rotation{yaw, 0}, NameTag: nameTag})
	if err != nil {
		return false
	}
	m.SetPersistent(true)
	dir, dist := d, d.Len()
	box := m.H().Type().BBox(m).Translate(pos)
	if dist <= 1e-6 {
		dir, dist = mgl64.Vec3{0, 1, 0}, box.Max()[1]-box.Min()[1]
	} else {
		dir = dir.Mul(1 / dist)
	}
	steps := 0.0
	for steps < dist && boxInBlocks(tx, box) {
		box = box.Translate(dir)
		steps++
	}
	if steps > 0 {
		m.Teleport(pos.Add(dir.Mul(steps)))
	}
	return true
}

// SpawnBabyFromEgg spawns a baby of target where it is, if target is a mob of the identifier whose
// definition makes babies (its entity_born event adds a minecraft:is_baby component), as Java Edition's
// SpawnEggItem.spawnOffspringFromSpawnEgg. Bedrock Edition has no such use of a spawn egg.
func (EggSpawner) SpawnBabyFromEgg(tx *world.Tx, id string, target world.Entity, nameTag string) bool {
	m, ok := Of(target)
	if !ok || m.Identifier() != id {
		return false
	}
	ev, ok := m.Definition().(interface{ Events() []string })
	if !ok || !slices.Contains(ev.Events(), EventBorn) {
		return false
	}
	baby, err := Spawn(tx, id, m.Position(), SpawnOptions{Event: EventBorn, NameTag: nameTag})
	if err != nil {
		return false
	}
	if !baby.Baby() {
		// The event did something else than make a baby.
		_ = baby.Close()
		return false
	}
	baby.SetPersistent(true)
	return true
}

// boxInBlocks reports whether box intersects the collision box of a block.
func boxInBlocks(tx *world.Tx, box cube.BBox) bool {
	lo, hi, r := box.Min(), box.Max(), tx.Range()
	for x := int(math.Floor(lo[0])); x <= int(math.Floor(hi[0])); x++ {
		for y := max(int(math.Floor(lo[1])), r[0]); y <= min(int(math.Floor(hi[1])), r[1]); y++ {
			for z := int(math.Floor(lo[2])); z <= int(math.Floor(hi[2])); z++ {
				p := cube.Pos{x, y, z}
				for _, bb := range tx.Block(p).Model().BBox(p, tx) {
					if bb.Translate(p.Vec3()).IntersectsWith(box) {
						return true
					}
				}
			}
		}
	}
	return false
}
