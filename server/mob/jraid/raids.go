package jraid

import (
	"math"
	"math/rand/v2"
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/google/uuid"
)

// RaidOmenTicks is the duration BadOmenMobEffect gives raid omen: 30 seconds.
const RaidOmenTicks = 600

// BadOmenTicks is OminousBottleAmplifier.EFFECT_DURATION: bad omen from an ominous bottle lasts 100 minutes.
const BadOmenTicks = 120000

// raidPOIRadius is the radius in which Raids.createOrExtendRaid averages the village's points of interest.
const raidPOIRadius = 64

// Raids is net.minecraft.world.entity.raid.Raids: the raids of a level, with the raid omen positions of its
// players (ServerPlayer.raidOmenPosition).
type Raids struct {
	raids  map[int]*Raid
	nextID int
	tick   int
	rand   *rand.Rand
	omen   map[uuid.UUID]cube.Pos
	dirty  bool
}

// NewRaids returns an empty Raids.
func NewRaids(r *rand.Rand) *Raids {
	return &Raids{raids: map[int]*Raid{}, nextID: 1, rand: r, omen: map[uuid.UUID]cube.Pos{}, dirty: true}
}

func (rs *Raids) setDirty() { rs.dirty = true }

// Dirty reports whether the raids changed since the last ClearDirty, so that they need saving.
func (rs *Raids) Dirty() bool { return rs.dirty }

// ClearDirty marks the raids saved.
func (rs *Raids) ClearDirty() { rs.dirty = false }

// ids are the raid ids in ascending order.
func (rs *Raids) ids() []int {
	ids := make([]int, 0, len(rs.raids))
	for id := range rs.raids {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// All returns the raids, by id.
func (rs *Raids) All() []*Raid {
	out := make([]*Raid, 0, len(rs.raids))
	for _, id := range rs.ids() {
		out = append(out, rs.raids[id])
	}
	return out
}

// Get is Raids.get.
func (rs *Raids) Get(id int) *Raid { return rs.raids[id] }

// Tick is Raids.tick: raids stop when the raids game rule is off, stopped raids are dropped and the others tick.
func (rs *Raids) Tick(l Level) {
	rs.tick++
	for _, id := range rs.ids() {
		raid := rs.raids[id]
		if !l.RaidsEnabled() {
			raid.Stop()
		}
		if raid.IsStopped() {
			delete(rs.raids, id)
			rs.setDirty()
			continue
		}
		raid.Tick(l, rs)
	}
	if rs.tick%200 == 0 {
		rs.setDirty()
	}
}

// RaidAt is ServerLevel.getRaidAt: the closest active raid centred within 96 blocks.
func (rs *Raids) RaidAt(pos cube.Pos) *Raid { return rs.NearbyRaid(pos, ValidRaidRadiusSqr) }

// IsRaided is ServerLevel.isRaided.
func (rs *Raids) IsRaided(pos cube.Pos) bool { return rs.RaidAt(pos) != nil }

// NearbyRaid is Raids.getNearbyRaid.
func (rs *Raids) NearbyRaid(pos cube.Pos, maxDistSqr int) *Raid {
	var closest *Raid
	closestDist := float64(maxDistSqr)
	for _, id := range rs.ids() {
		raid := rs.raids[id]
		if d := distSqr(raid.center, pos); raid.IsActive() && d < closestDist {
			closest, closestDist = raid, d
		}
	}
	return closest
}

// CreateOrExtendRaid is Raids.createOrExtendRaid (with Paper's isInProgress check): the raid of the village around
// pos, centred on the average of its occupied points of interest within 64 blocks, is created or, while it runs
// below the highest omen level, extended by the player's raid omen.
func (rs *Raids) CreateOrExtendRaid(l Level, p Player, pos cube.Pos) *Raid {
	if p.Spectator() || !l.RaidsEnabled() || !l.CanStartRaid(pos) {
		return nil
	}
	pois := l.OccupiedVillagePOI(pos, raidPOIRadius)
	center := pos
	if len(pois) > 0 {
		var x, y, z float64
		for _, q := range pois {
			x, y, z = x+float64(q[0]), y+float64(q[1]), z+float64(q[2])
		}
		n := 1.0 / float64(len(pois))
		center = cube.Pos{int(math.Floor(x * n)), int(math.Floor(y * n)), int(math.Floor(z * n))}
	}
	raid := rs.RaidAt(center)
	if raid == nil {
		raid = NewRaid(center, l.Difficulty(), rs.rand)
	}
	if !raid.IsStarted() || raid.IsInProgress() && raid.RaidOmenLevel() < MaxRaidOmenLevel {
		if !raid.IsStarted() && !slices.Contains(rs.All(), raid) {
			rs.nextID++
			raid.id = rs.nextID
			rs.raids[raid.id] = raid
		}
		raid.AbsorbRaidOmen(p)
	}
	rs.setDirty()
	return raid
}

// BadOmenTick is BadOmenMobEffect.applyEffectTick for a player with bad omen of the amplifier, each tick: in a
// village without a raid at the highest omen level, bad omen turns into raid omen of the same amplifier for 30
// seconds, remembering where (the player's raid omen position). It reports whether bad omen stays.
func (rs *Raids) BadOmenTick(l Level, p Player, amplifier int) (keep bool) {
	if p.Spectator() || l.Difficulty() == Peaceful || !IsVillage(l, p.BlockPos()) {
		return true
	}
	raid := rs.RaidAt(p.BlockPos())
	if raid != nil && raid.RaidOmenLevel() >= MaxRaidOmenLevel {
		return true
	}
	p.AddRaidOmen(amplifier, RaidOmenTicks)
	rs.omen[p.UUID()] = p.BlockPos()
	return false
}

// RaidOmenTick is RaidOmenMobEffect.applyEffectTick: on the last tick of raid omen (remainingTicks 1), the raid of
// the player's raid omen position is created or extended. It reports whether raid omen stays.
func (rs *Raids) RaidOmenTick(l Level, p Player, remainingTicks int) (keep bool) {
	if remainingTicks != 1 || p.Spectator() {
		return true
	}
	pos, ok := rs.omen[p.UUID()]
	if !ok {
		return true
	}
	rs.CreateOrExtendRaid(l, p, pos)
	delete(rs.omen, p.UUID())
	return false
}

// OmenPosition is ServerPlayer.getRaidOmenPosition.
func (rs *Raids) OmenPosition(id uuid.UUID) (cube.Pos, bool) {
	pos, ok := rs.omen[id]
	return pos, ok
}

// SetOmenPosition is ServerPlayer.setRaidOmenPosition, for players loaded with one.
func (rs *Raids) SetOmenPosition(id uuid.UUID, pos cube.Pos) { rs.omen[id] = pos }
