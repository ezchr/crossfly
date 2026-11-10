//go:build crossfly_full

package javamob

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// SpawnOptions are the options of Spawn.
type SpawnOptions struct {
	Rotation cube.Rotation
	NameTag  string
	// NoFinalize skips finalizeSpawn (a mob made by a conversion that copies its state itself).
	NoFinalize bool
	// Configure, if set, runs before finalizeSpawn.
	Configure func(m *Mob)
	// GroupData is the SpawnGroupData of a pack spawn: it is passed to the behaviour's GroupFinalizer, and the
	// value it returns is put back here for the next member of the pack.
	GroupData *any
}

// Spawn adds a Java mob of the type at pos and finalizes it for the reason (EntityType.spawn: create, then
// Mob.finalizeSpawn, then addFreshEntity). Java mob mode must be installed.
func Spawn(tx *world.Tx, id string, pos mgl64.Vec3, reason SpawnReason, opts SpawnOptions) (*Mob, error) {
	if _, ok := TypeOf(id); !ok {
		return nil, fmt.Errorf("javamob: no Java type %q", id)
	}
	var out *Mob
	_, err := mob.Spawn(tx, id, pos, mob.SpawnOptions{Rotation: opts.Rotation, NameTag: opts.NameTag, NoEvent: true, Configure: func(bm *mob.Mob) {
		m, ok := wrap(bm)
		if !ok {
			return
		}
		m.s.reason = reason
		if opts.Configure != nil {
			opts.Configure(m)
		}
		if !opts.NoFinalize {
			m.s.group = opts.GroupData
			m.finalizeSpawn(reason)
			m.s.group = nil
		} else {
			m.s.finalized = true
		}
		out = m
	}})
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, fmt.Errorf("javamob: %v is not a Java mob (Install not called?)", id)
	}
	return out, nil
}

// finalizeSpawn is Mob.finalizeSpawn and the behaviour's override, once per mob.
func (m *Mob) finalizeSpawn(reason SpawnReason) {
	s := m.s
	if s.finalized {
		return
	}
	s.finalized = true
	s.reason = reason
	r := m.Rand()
	fr := s.attrs.Instance(AttrFollowRange)
	if !fr.HasModifier("minecraft:random_spawn_bonus") {
		fr.AddModifier(Modifier{ID: "minecraft:random_spawn_bonus", Amount: 0.11485000000000001 * (r.Float64() - r.Float64()), Op: OpAddMultipliedBase})
	}
	s.leftHanded = r.Float32() < 0.05
	if f, ok := s.b.(SpawnFinalizer); ok {
		f.FinalizeSpawn(m, reason)
	}
	if f, ok := s.b.(GroupFinalizer); ok {
		var group any
		if s.group != nil {
			group = *s.group
		}
		group = f.FinalizeSpawnGroup(m, reason, group)
		if s.group != nil {
			*s.group = group
		}
	}
	m.syncAttributes()
}

// syncAttributes shows the attributes the clients see (health, speed) on the Bedrock side of the mob.
func (m *Mob) syncAttributes() {
	maxH := m.Attr(AttrMaxHealth)
	if m.MaxHealth() != maxH {
		hp := m.Health()
		m.SetMaxHealth(maxH)
		if hp > maxH {
			m.SetHealth(maxH)
		}
	}
	m.Mob.SetSpeed(m.Attr(AttrMovementSpeed))
}

// SyncAttributes is to be called after changing max health or movement speed attributes outside finalizeSpawn, so
// the clients see the change.
func (m *Mob) SyncAttributes() { m.syncAttributes() }

// CheckSpawnObstruction is Mob.checkSpawnObstruction: no liquid and no block or entity in the mob's box.
func (m *Mob) CheckSpawnObstruction() bool {
	bb := m.BBox()
	tx := m.Tx()
	for pos := range cube.Range3D(cube.PosFromVec3(bb.Min()), cube.PosFromVec3(bb.Max())) {
		if _, ok := tx.Liquid(pos); ok {
			return false
		}
	}
	return m.isFree(mgl64.Vec3{})
}

// MaxSpawnClusterSize is Mob.getMaxSpawnClusterSize (4 unless the behaviour is a GroupSizer).
func (m *Mob) MaxSpawnClusterSize() int {
	if g, ok := m.s.b.(GroupSizer); ok {
		return g.MaxSpawnClusterSize()
	}
	return 4
}

// IsMaxGroupSizeReached is Mob.isMaxGroupSizeReached (false unless the behaviour is a GroupSizer).
func (m *Mob) IsMaxGroupSizeReached(n int) bool {
	if g, ok := m.s.b.(GroupSizer); ok {
		return g.IsMaxGroupSizeReached(n)
	}
	return false
}
