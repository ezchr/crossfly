//go:build crossfly_full

package jvillage

import (
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/jbrain"
	"github.com/df-mc/dragonfly/server/mob/jbrain/jmob"
	"github.com/df-mc/dragonfly/server/mob/jpoi"
	"github.com/df-mc/dragonfly/server/world"
)

type sensorFunc struct {
	req   []jbrain.MemoryID
	sense func(c *jbrain.Ctx, m *javamob.Mob)
}

func (s sensorFunc) Requires() []jbrain.MemoryID         { return s.req }
func (s sensorFunc) Sense(c *jbrain.Ctx, m *javamob.Mob) { s.sense(c, m) }

func sensorType(name, key string, rate int, f func() jmob.Sensor) *jbrain.SensorType[*javamob.Mob] {
	return &jbrain.SensorType[*javamob.Mob]{Name: "minecraft:" + name, Key: key, ScanRate: rate, New: f}
}

// hostileDistances is VillagerHostilesSensor.ACCEPTABLE_DISTANCE_FROM_HOSTILES.
var hostileDistances = map[string]float64{
	"minecraft:drowned": 8, "minecraft:evoker": 12, "minecraft:husk": 8, "minecraft:illusioner": 12,
	"minecraft:pillager": 15, "minecraft:ravager": 12, "minecraft:vex": 8, "minecraft:vindicator": 10,
	"minecraft:zoglin": 10, "minecraft:zombie": 8, "minecraft:zombie_villager_v2": 8,
}

// VillagerHostiles is SensorType.VILLAGER_HOSTILES: the closest visible
// hostile within its type's distance.
var VillagerHostiles = jmob.VisibleSensor("villager_hostiles", "villagerhostilessensor", jbrain.MemNearestHostile, func(c *jbrain.Ctx, m *javamob.Mob, e world.Entity) bool {
	d, ok := hostileDistances[e.H().Type().EncodeEntity()]
	return ok && e.Position().Sub(m.Position()).LenSqr() <= d*d
})

// isVillagerBaby reports whether e is a baby villager.
func isVillagerBaby(e world.Entity) bool {
	jm, ok := javamob.Of(e)
	if !ok {
		return false
	}
	v, ok := Of(jm)
	return ok && v.Baby()
}

// VillagerBabies is SensorType.VILLAGER_BABIES.
var VillagerBabies = sensorType("villager_babies", "villagerbabiessensor", 0, func() jmob.Sensor {
	return sensorFunc{req: []jbrain.MemoryID{jbrain.MemVisibleVillagerBabies.ID()}, sense: func(c *jbrain.Ctx, m *javamob.Mob) {
		v, _ := jbrain.Get(c.Mem, jbrain.MemNearestVisibleLivingEntities)
		babies := v.FindAll(c.Tx, func(h *world.EntityHandle) bool {
			e, ok := h.Entity(c.Tx)
			return ok && isVillagerBaby(e)
		})
		jbrain.Set(c.Mem, jbrain.MemVisibleVillagerBabies, babies)
	}}
})

// GolemDetected is SensorType.GOLEM_DETECTED (GolemSensor, every 200 ticks).
var GolemDetected = sensorType("golem_detected", "golemsensor", 200, func() jmob.Sensor {
	return sensorFunc{req: []jbrain.MemoryID{jbrain.MemNearestLivingEntities.ID(), jbrain.MemGolemDetectedRecently.ID()}, sense: func(c *jbrain.Ctx, m *javamob.Mob) {
		list, _ := jbrain.Get(c.Mem, jbrain.MemNearestLivingEntities)
		for _, h := range list {
			if h.Type().EncodeEntity() == IronGolemID {
				jbrain.SetWithExpiry(c.Mem, jbrain.MemGolemDetectedRecently, true, 599)
				return
			}
		}
	}}
})

// SecondaryPois is SensorType.SECONDARY_POIS: the profession's secondary
// work blocks (farmland) within 4/2/4 (scan rate 40; Paper keeps 40).
var SecondaryPois = sensorType("secondary_pois", "secondarypoisensor", 40, func() jmob.Sensor {
	return sensorFunc{req: []jbrain.MemoryID{jbrain.MemSecondaryJobSite.ID()}, sense: func(c *jbrain.Ctx, m *javamob.Mob) {
		v, ok := Of(m)
		if !ok {
			return
		}
		var sites []jbrain.GlobalPos
		if sec := v.profession().SecondaryPoi; len(sec) > 0 {
			center := BlockPos(m)
			dim := c.Tx.World().Dimension()
			for x := -4; x <= 4; x++ {
				for y := -2; y <= 2; y++ {
					for z := -4; z <= 4; z++ {
						p := center.Add(cube.Pos{x, y, z})
						if slices.Contains(sec, blockName(c.Tx.Block(p))) {
							sites = append(sites, jbrain.GlobalPos{Dim: dim, Pos: p})
						}
					}
				}
			}
		}
		jbrain.Set(c.Mem, jbrain.MemSecondaryJobSite, sites)
	}}
})

// nearestBed is NearestBedSensor (babies look for beds to jump on).
type nearestBed struct {
	cache   map[cube.Pos]int64
	tried   int
	lastUpd int64
}

// NearestBed is SensorType.NEAREST_BED.
var NearestBed = sensorType("nearest_bed", "nearestbedsensor", 20, func() jmob.Sensor { return &nearestBed{cache: map[cube.Pos]int64{}} })

func (s *nearestBed) Requires() []jbrain.MemoryID {
	return []jbrain.MemoryID{jbrain.MemNearestBed.ID()}
}

func (s *nearestBed) Sense(c *jbrain.Ctx, m *javamob.Mob) {
	v, ok := Of(m)
	if !ok || !v.Baby() {
		return
	}
	s.tried = 0
	s.lastUpd = c.Time + int64(c.Rand.IntN(20))
	pm := jpoi.For(c.Tx)
	test := func(p cube.Pos) bool {
		if _, ok := s.cache[p]; ok {
			return false
		}
		s.tried++
		if s.tried >= 5 {
			return false
		}
		s.cache[p] = s.lastUpd + 40
		return true
	}
	pois := pm.FindAllWithType(c.Tx, jpoi.Is(jpoi.Home), test, BlockPos(m), 48, jpoi.AnyOccupancy)
	if target, ok := pathToPois(m, pois); ok {
		if _, ok := pm.Type(c.Tx, target); ok {
			jbrain.Set(c.Mem, jbrain.MemNearestBed, target)
		}
	} else if s.tried < 5 {
		for p, t := range s.cache {
			if t < s.lastUpd {
				delete(s.cache, p)
			}
		}
	}
}

// pathToPois is AcquirePoi.findPathToPois: a path to any of the PoIs within
// the largest valid range; the reached target when it can be reached.
func pathToPois(m *javamob.Mob, pois []jpoi.Found) (cube.Pos, bool) {
	if len(pois) == 0 {
		return cube.Pos{}, false
	}
	maxRange := 1
	targets := make([]cube.Pos, 0, len(pois))
	for _, p := range pois {
		maxRange = max(maxRange, p.Type.ValidRange)
		targets = append(targets, p.Pos)
	}
	path := m.Navigation().CreatePathToAny(m, targets, maxRange)
	if path == nil || !path.CanReach() {
		return cube.Pos{}, false
	}
	return path.Target(), true
}

// NearestItems is SensorType.NEAREST_ITEMS (NearestItemSensor): the closest
// visible item within 32 blocks the mob wants to pick up.
var NearestItems = sensorType("nearest_items", "nearestitemsensor", 0, func() jmob.Sensor {
	return sensorFunc{req: []jbrain.MemoryID{jbrain.MemNearestVisibleWantedItem.ID()}, sense: func(c *jbrain.Ctx, m *javamob.Mob) {
		v, ok := Of(m)
		if !ok {
			return
		}
		pos := m.Position()
		type cand struct {
			e world.Entity
			d float64
		}
		var items []cand
		for e := range c.Tx.EntitiesWithin(m.BBox().GrowVec3([3]float64{32, 16, 32})) {
			ent, ok := e.(*entity.Ent)
			if !ok {
				continue
			}
			b, ok := ent.Behaviour().(*entity.ItemBehaviour)
			if !ok {
				continue
			}
			d := e.Position().Sub(pos).LenSqr()
			if d < 32*32 && v.WantsToPickUp(b.Item()) {
				items = append(items, cand{e, d})
			}
		}
		slices.SortStableFunc(items, func(a, b cand) int {
			switch {
			case a.d < b.d:
				return -1
			case a.d > b.d:
				return 1
			}
			return 0
		})
		for _, it := range items {
			if m.HasLineOfSight(it.e) {
				jbrain.Set(c.Mem, jbrain.MemNearestVisibleWantedItem, it.e.H())
				return
			}
		}
		c.Mem.Erase(jbrain.MemNearestVisibleWantedItem.ID())
	}}
})
