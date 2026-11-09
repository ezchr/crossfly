//go:build crossfly_full

package spawn

import (
	"bytes"
	"encoding/json"
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
)

func jsonUnmarshal(raw []byte, v any) error { return json.Unmarshal(raw, v) }

// emptyJSON reports whether a filter is absent, null, {} or [] (FilterGroup::empty).
func emptyJSON(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) == 0 || bytes.Equal(t, []byte("null")) || bytes.Equal(t, []byte("{}")) || bytes.Equal(t, []byte("[]"))
}

// mobInfo is what this package keeps on every mob (mob.Data).
type mobInfo struct {
	// surface is Mob::isSurfaceMob: naturally spawned underground mobs are
	// counted (and capped) apart from surface ones. Mobs from elsewhere
	// count as surface mobs.
	surface bool
	// natural is Mob::isNaturallySpawned.
	natural bool
	// pool is the population_control of the mob's identifier ("" if none).
	pool     string
	poolSeen bool
}

type infoKey struct{}

func infoOf(m *mob.Mob) *mobInfo {
	if v, ok := m.Data(infoKey{}); ok {
		return v.(*mobInfo)
	}
	in := &mobInfo{surface: true}
	m.SetData(infoKey{}, in)
	return in
}

// InactivityTicks is how long the mob has been inactive (BDS
// Mob::getNoActionTime): ticks since it was last hurt or had a player within
// its despawn min_distance. The inactivity_timer filter test reads it in
// seconds (/20).
func InactivityTicks(m *mob.Mob) int { return m.InactivityTicks() }

// ResetInactivity resets the mob's inactivity timer (Mob::resetNoActionTime).
func ResetInactivity(m *mob.Mob) { m.ResetInactivity() }

// NaturallySpawned reports whether the natural spawner spawned the mob.
func NaturallySpawned(m *mob.Mob) bool { return infoOf(m).natural }

// mobSystem runs for every mob in StageDespawn, before the despawn
// component: it advances the inactivity timer, counts the spawnable mobs
// ticked for the global cap and makes name-tagged mobs persistent (a named
// mob never despawns, like a NameTagItem use in BDS).
func mobSystem(m *mob.Mob) {
	// The inactivity timer itself counts in the mob core (Mob.InactivityTicks)
	// and is reset there when the mob is hurt.
	in := infoOf(m)
	if !in.poolSeen {
		in.pool, in.poolSeen = PopulationControl(m.Identifier()), true
	}
	tx := m.Tx()
	if in.pool != "" {
		stateOf(tx.World()).countSpawnable(tx.CurrentTick())
	}
	if !m.Persistent() && m.NameTag() != "" {
		m.SetPersistent(true)
	}
}

// despawnDef is DespawnDefinition (minecraft:despawn).
type despawnDef struct {
	filter             mob.Filter
	hasFilter          bool
	removeChildren     bool
	inactivityTimer    int // min_range_inactivity_timer, seconds
	fromInactivity     bool
	randomChance       int // min_range_random_chance, 1 in n per tick
	fromChance         bool
	fromSimulationEdge bool
	fromDistance       bool
	minDist, maxDist   int
}

func parseDespawn(raw json.RawMessage) (despawnDef, error) {
	d := despawnDef{inactivityTimer: 30, fromInactivity: true, randomChance: 800, fromChance: true, fromSimulationEdge: true, minDist: 32, maxDist: 128}
	var m map[string]json.RawMessage
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &m); err != nil {
			return d, err
		}
	}
	if f, ok := m["filters"]; ok && !emptyJSON(f) {
		flt, err := mob.CompileFilter(f)
		if err != nil {
			return d, err
		}
		d.filter, d.hasFilter = flt, true
	}
	d.removeChildren = boolOr(m["remove_child_entities"], false)
	d.inactivityTimer = intOr(m["min_range_inactivity_timer"], 30)
	d.fromInactivity = boolOr(m["despawn_from_inactivity"], true)
	d.randomChance = intOr(m["min_range_random_chance"], 800)
	d.fromChance = boolOr(m["despawn_from_chance"], true)
	d.fromSimulationEdge = boolOr(m["despawn_from_simulation_edge"], true)
	if raw, ok := m["despawn_from_distance"]; ok {
		d.fromDistance = true
		var dist map[string]json.RawMessage
		_ = json.Unmarshal(raw, &dist)
		d.minDist = intOr(dist["min_distance"], 32)
		d.maxDist = intOr(dist["max_distance"], 128)
	}
	return d, nil
}

// despawnComponent is DespawnComponent, ticked by DespawnSystem every tick.
type despawnComponent struct {
	def  despawnDef
	info *mobInfo
}

func newDespawn(_ string, raw json.RawMessage) (mob.Component, error) {
	d, err := parseDespawn(raw)
	if err != nil {
		return nil, err
	}
	return &despawnComponent{def: d}, nil
}

// Attach ...
func (c *despawnComponent) Attach(m *mob.Mob) { c.info = infoOf(m) }

// Detach ...
func (c *despawnComponent) Detach(*mob.Mob) {}

// TickStage ...
func (c *despawnComponent) TickStage() mob.Stage { return mob.StageDespawn }

// EncodeNBT saves whether the mob was a natural surface/underground spawn.
func (c *despawnComponent) EncodeNBT() map[string]any {
	if c.info == nil || !c.info.natural {
		return nil
	}
	s := byte(0)
	if c.info.surface {
		s = 1
	}
	return map[string]any{"Surface": s, "Natural": byte(1)}
}

// DecodeNBT ...
func (c *despawnComponent) DecodeNBT(m *mob.Mob, data map[string]any) {
	in := infoOf(m)
	if v, ok := data["Natural"].(byte); ok && v == 1 {
		in.natural = true
		if s, ok := data["Surface"].(byte); ok {
			in.surface = s == 1
		}
	}
}

// Tick is DespawnComponent::tick.
func (c *despawnComponent) Tick(m *mob.Mob) {
	if m.Dead() || m.NameTag() != "" {
		return
	}
	tx := m.Tx()
	st := stateOf(tx.World())
	if st.noDesp.Load() {
		return
	}
	cfg := st.config()
	st.refreshPlayers(tx, cfg)
	if len(st.players) == 0 {
		// Dimension::fetchAnyInteractablePlayer(pos, FLT_MAX) found nobody.
		return
	}
	d := &c.def
	if d.hasFilter {
		if chunksLoaded(tx, cube.PosFromVec3(m.Pos()), 1) && d.filter.Test(m.Ctx()) {
			_ = m.Close()
		}
		return
	}
	if !d.fromDistance && !d.fromInactivity && !d.fromChance && !d.fromSimulationEdge {
		_ = m.Close()
		return
	}
	if standardDespawn(m, d, st, cfg) {
		_ = m.Close()
	}
}

// standardDespawn is DespawnComponent::_tryStandardDespawnRules.
func standardDespawn(m *mob.Mob, d *despawnDef, st *worldState, cfg *Config) bool {
	if m.Persistent() {
		return false
	}
	pos := m.Pos()
	despawn := true
	if d.fromInactivity && m.InactivityTicks() < 20*d.inactivityTimer {
		despawn = false
	} else if d.fromChance {
		n := int(math.Abs(float64(d.randomChance)))
		despawn = n == 0 || m.Rand().IntN(n) == 0
	}
	if d.fromDistance {
		near := st.anyPlayerWithin(pos, float64(d.minDist))
		if near {
			m.ResetInactivity()
		}
		despawn = despawn && !near
	}
	if despawn {
		if d.fromDistance || d.fromInactivity || d.fromChance {
			return true
		}
	} else if d.fromDistance {
		return edgeDespawn(m, d, st, cfg)
	}
	if !d.fromSimulationEdge {
		return farDespawn(m, d, st)
	}
	return edgeDespawn(m, d, st, cfg)
}

// edgeDespawn is the part of the standard rules that needs the simulation
// distance. With the low-end distance of 4 a mob despawns at once when no
// player is within max_distance capped at 44 blocks (the farthest it can
// spawn; the float the dump passes in a register is not visible, so the cap
// is inferred from LOW_END_MAX_SPAWN_DISTANCE).
func edgeDespawn(m *mob.Mob, d *despawnDef, st *worldState, cfg *Config) bool {
	if cfg.SimulationDistance <= 4 {
		return !st.anyPlayerWithin(m.Pos(), float64(min(d.maxDist, lowEndMaxSpawnDistance)))
	}
	return farDespawn(m, d, st)
}

func farDespawn(m *mob.Mob, d *despawnDef, st *worldState) bool {
	pos := cube.PosFromVec3(m.Pos())
	if d.fromSimulationEdge {
		if chunksLoaded(m.Tx(), pos, 1) && st.untickedNeighbour(chunkOf(pos)) {
			return true
		}
	}
	if d.fromDistance && !st.anyPlayerWithin(m.Pos(), float64(d.maxDist)) {
		return true
	}
	return false
}

// chunksLoaded is BlockSource::areChunksFullyLoaded(pos, r).
func chunksLoaded(tx *world.Tx, pos cube.Pos, r int) bool {
	for dx := -r; dx <= r; dx++ {
		for dz := -r; dz <= r; dz++ {
			if _, ok := tx.BlockLoaded(cube.Pos{pos[0] + dx*16, pos[1], pos[2] + dz*16}); !ok {
				return false
			}
		}
	}
	return true
}

// persistentComponent is minecraft:persistent: the mob never despawns while
// it has the component.
type persistentComponent struct{ set bool }

func newPersistent(string, json.RawMessage) (mob.Component, error) {
	return &persistentComponent{}, nil
}

// Attach ...
func (p *persistentComponent) Attach(m *mob.Mob) {
	if !m.Persistent() {
		m.SetPersistent(true)
		p.set = true
	}
}

// Detach ...
func (p *persistentComponent) Detach(m *mob.Mob) {
	if p.set {
		m.SetPersistent(false)
	}
}
