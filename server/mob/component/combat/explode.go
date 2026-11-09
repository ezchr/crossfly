//go:build crossfly_full

package combat

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// MobGriefing reports whether mobs may break blocks in tx's world (the
// mobGriefing game rule, which Dragonfly does not have). Servers may
// replace it at start-up. destroy_affected_by_griefing explosions and
// annotation.break_door check it.
var MobGriefing = func(tx *world.Tx) bool { return true }

// Explode is minecraft:explode (BDS ExplodeComponent + ExplodeSystem): with
// fuse_lit the fuse (fuse_length seconds, rolled) counts down from the moment
// the component attaches, then the mob explodes with power and is removed.
// A creeper's "exploding" group adds it; removing the group cancels it.
type Explode struct {
	fuseLength      sense.Range
	fuseLit         bool
	power           float64
	causesFire      bool
	breaksBlocks    bool
	griefingBlocks  bool
	griefingFire    bool
	allowUnderwater bool

	fuse   int // -1: not lit
	loaded bool
}

func newExplode(_ string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		FuseLength                json.RawMessage `json:"fuse_length"`
		FuseLit                   bool            `json:"fuse_lit"`
		Power                     *float64        `json:"power"`
		CausesFire                bool            `json:"causes_fire"`
		BreaksBlocks              *bool           `json:"breaks_blocks"`
		DestroyAffectedByGriefing bool            `json:"destroy_affected_by_griefing"`
		FireAffectedByGriefing    bool            `json:"fire_affected_by_griefing"`
		AllowUnderwater           bool            `json:"allow_underwater"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("minecraft:explode: %w", err)
	}
	fl, err := sense.ParseRange(v.FuseLength, sense.Range{})
	if err != nil {
		return nil, fmt.Errorf("minecraft:explode: fuse_length: %w", err)
	}
	e := &Explode{fuseLength: fl, fuseLit: v.FuseLit, power: 3, causesFire: v.CausesFire, breaksBlocks: true,
		griefingBlocks: v.DestroyAffectedByGriefing, griefingFire: v.FireAffectedByGriefing, allowUnderwater: v.AllowUnderwater, fuse: -1}
	if v.Power != nil {
		e.power = *v.Power
	}
	if v.BreaksBlocks != nil {
		e.breaksBlocks = *v.BreaksBlocks
	}
	return e, nil
}

// Attach lights the fuse when fuse_lit.
func (e *Explode) Attach(m *mob.Mob) {
	if e.fuseLit {
		e.Ignite(m)
	}
}

// Detach puts the fuse out.
func (e *Explode) Detach(m *mob.Mob) {
	e.fuse = -1
	m.UpdateMeta(func(meta *mob.Meta) { meta.Ignited, meta.FuseTicks = false, 0 })
}

// Ignite lights the fuse (flint and steel, fuse_lit).
func (e *Explode) Ignite(m *mob.Mob) {
	if e.fuse >= 0 {
		return
	}
	e.fuse = int(math.Round(e.fuseLength.Roll(m.Rand()) * 20))
	ticks := int32(e.fuse)
	m.UpdateMeta(func(meta *mob.Meta) { meta.Ignited, meta.FuseTicks = true, ticks })
	m.Tx().PlaySound(m.Position(), sound.TNT{})
}

// Lit reports whether the fuse burns, FuseLeft how many ticks are left.
func (e *Explode) Lit() bool     { return e.fuse >= 0 }
func (e *Explode) FuseLeft() int { return e.fuse }

// tick counts the fuse down (system combat/explode).
func (e *Explode) tick(m *mob.Mob) {
	if e.fuse < 0 || m.Dead() {
		return
	}
	if e.fuse--; e.fuse > 0 {
		return
	}
	e.Detonate(m)
}

// Detonate explodes at the mob and removes it.
func (e *Explode) Detonate(m *mob.Mob) {
	tx := m.Tx()
	e.fuse = -1
	fire := e.causesFire && (!e.griefingFire || MobGriefing(tx))
	blocks := e.breaksBlocks && (!e.griefingBlocks || MobGriefing(tx))
	if !e.allowUnderwater && m.Physics().InWater {
		blocks = false
	}
	src := world.EntityExplosionSource{Entity: m, ExplosionSize: e.power}
	if blocks {
		block.ExplosionConfig{SpawnFire: fire, Source: m}.Explode(tx, src)
	} else {
		// Dragonfly explosions always break blocks: hurt entities only.
		ExplodeEntitiesOnly(tx, m, e.power)
	}
	_ = m.Close()
}

// EncodeNBT saves a lit fuse (BDS Fuse/IsFuseLit).
func (e *Explode) EncodeNBT() map[string]any {
	if e.fuse < 0 {
		return nil
	}
	return map[string]any{"Fuse": int32(e.fuse), "IsFuseLit": byte(1)}
}

// DecodeNBT restores it.
func (e *Explode) DecodeNBT(m *mob.Mob, d map[string]any) {
	if lit, _ := d["IsFuseLit"].(byte); lit == 1 {
		if f, ok := d["Fuse"].(int32); ok {
			e.fuse = int(f)
			m.UpdateMeta(func(meta *mob.Meta) { meta.Ignited, meta.FuseTicks = true, f })
		}
	}
}

// ExplodeEntitiesOnly is an explosion of power at src that hurts and pushes
// entities like block.ExplosionConfig but leaves blocks alone (for
// breaks_blocks false or mobGriefing off).
func ExplodeEntitiesOnly(tx *world.Tx, src world.Entity, power float64) {
	origin := src.Position()
	d := power * 2
	box := cube.Box(origin[0]-d-1, origin[1]-d-1, origin[2]-d-1, origin[0]+d+1, origin[1]+d+1, origin[2]+d+1)
	es := world.EntityExplosionSource{Entity: src, ExplosionSize: power}
	var hit []world.Entity
	for e := range tx.EntitiesWithin(box) {
		dist := e.Position().Sub(origin).Len()
		if e.H() == src.H() || dist == 0 || ExplosionDistance(e, dist) > d {
			continue
		}
		hit = append(hit, e)
	}
	for _, e := range hit {
		ex, ok := e.(block.ExplodableEntity)
		if !ok {
			continue
		}
		impact := (1 - ExplosionDistance(e, e.Position().Sub(origin).Len())/d) * exposure(tx, origin, e)
		if impact > 0 {
			ex.Explode(es, impact)
		}
	}
	tx.AddParticle(origin, particle.HugeExplosion{})
	tx.PlaySound(origin, sound.Explosion{})
}

// exposure is the share of e's box corners and centre the origin can see.
func exposure(tx *world.Tx, origin mgl64.Vec3, e world.Entity) float64 {
	b := sense.BBox(e)
	pts := []mgl64.Vec3{b.Min().Add(b.Max()).Mul(0.5)}
	for _, x := range []float64{b.Min()[0], b.Max()[0]} {
		for _, y := range []float64{b.Min()[1], b.Max()[1]} {
			for _, z := range []float64{b.Min()[2], b.Max()[2]} {
				pts = append(pts, mgl64.Vec3{x, y, z})
			}
		}
	}
	seen := 0
	for _, p := range pts {
		if sense.LineOfSight(tx, origin, p) {
			seen++
		}
	}
	return float64(seen) / float64(len(pts))
}

func init() { block.EntityExplosionDistance = ExplosionDistance }

// ExplosionDistance is the distance Explosion::explode uses for a mob dist
// blocks from the centre: dist minus half the mob's width, at least 0
// (players and other entities keep dist).
func ExplosionDistance(e world.Entity, dist float64) float64 {
	m, ok := mob.Of(e)
	if !ok {
		return dist
	}
	return math.Max(dist-m.BBox().Width()/2, 0)
}
