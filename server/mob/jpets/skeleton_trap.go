//go:build crossfly_full

package jpets

import (
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// skeletonTrap is SkeletonTrapGoal: when a player comes within 10 blocks of
// a trap skeleton horse, lightning strikes (only for show) and four
// skeleton horsemen appear: the trap horse and three more, each ridden by a
// skeleton in an iron helmet; the horses are tame adults.
type skeletonTrap struct{ h *Equine }

func (g *skeletonTrap) Flags() javamob.Flags { return 0 }

// CanUse is Level.hasNearbyAlivePlayer(10) (Paper: players that affect
// spawning, so not spectators).
func (g *skeletonTrap) CanUse(m *javamob.Mob) bool {
	for p := range m.Tx().Players() {
		pl, ok := p.(*player.Player)
		if !ok || pl.Dead() || pl.GameMode() == world.GameModeSpectator {
			continue
		}
		if distSq(pl.Position(), m.Position()) < 100 {
			return true
		}
	}
	return false
}

func (g *skeletonTrap) Tick(m *javamob.Mob) {
	g.h.SetTrap(m, false)
	g.h.setTamed(m, true)
	g.h.Animal.SetAge(0)
	tx := m.Tx()
	tx.AddEntity(entity.NewLightningWithDamage(world.EntitySpawnOpts{Position: m.Position()}, 0, false, 0))
	if !trapSkeleton(m) {
		return
	}
	for range 3 {
		other, err := javamob.Spawn(tx, "minecraft:skeleton_horse", m.Position(), javamob.SpawnTriggered, javamob.SpawnOptions{})
		if err != nil {
			continue
		}
		other.SetPersistenceRequired()
		if b, ok := other.Behaviour().(*Equine); ok {
			b.setTamed(other, true)
			b.Animal.SetAge(0)
		}
		if trapSkeleton(other) {
			r := m.Rand()
			other.Push(mgl64.Vec3{(r.Float64() - r.Float64()) * 1.1485, 0, (r.Float64() - r.Float64()) * 1.1485})
		}
	}
}

// trapSkeleton is createSkeleton + startRiding: a persistent skeleton with an
// iron helmet if it has none (Crossfly does not enchant the gear yet).
func trapSkeleton(horse *javamob.Mob) bool {
	s, err := javamob.Spawn(horse.Tx(), "minecraft:skeleton", horse.Position(), javamob.SpawnTriggered, javamob.SpawnOptions{})
	if err != nil {
		return false
	}
	s.SetPersistenceRequired()
	if s.ItemBySlot(javamob.SlotHead).Empty() {
		if it, ok := world.ItemByName("minecraft:iron_helmet", 0); ok {
			s.SetItemSlot(javamob.SlotHead, item.NewStack(it, 1))
		}
	}
	return s.StartRiding(horse)
}
