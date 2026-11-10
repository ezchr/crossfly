package jraid

import (
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

// BellBlockEntity constants.
const (
	bellDuration              = 50
	bellMinTicksBetweenSearch = 60
	bellMaxResonationTicks    = 40
	bellTicksBeforeResonation = 5
	bellSearchRadius          = 48
	bellHearRadius            = 32
	bellHighlightRadius       = 48
	// BellGlowTicks is how long a raider revealed by a bell glows (BellBlockEntity.glow: GLOWING for 60 ticks).
	BellGlowTicks = 60
)

// BellEntity is a living entity near a bell.
type BellEntity interface {
	Alive() bool
	Removed() bool
	Position() mgl64.Vec3
	// IsRaider reports whether the entity is in the raiders entity tag (evoker, pillager, ravager, vindicator,
	// illusioner, witch).
	IsRaider() bool
	// HearBell sets the entity's HEARD_BELL_TIME memory (villagers) to the game time.
	HearBell(gameTime int64)
	// Glow gives the entity the glowing effect for ticks.
	Glow(ticks int)
}

// BellLevel is what a bell needs of its level.
type BellLevel interface {
	GameTime() int64
	// LivingEntitiesWithin are the living entities whose boxes intersect box (Level.getEntitiesOfClass).
	LivingEntitiesWithin(box cube.BBox) []BellEntity
	// PlayResonate plays block.bell.resonate at the bell.
	PlayResonate(pos cube.Pos)
}

// Bell is the server side of net.minecraft.world.level.block.entity.BellBlockEntity: ringing makes villagers within
// 32 blocks hear it, and if a raider is within 32 blocks the bell resonates and, 40 ticks later, raiders within 48
// blocks glow.
type Bell struct {
	Pos             cube.Pos
	lastRing        int64
	Ticks           int
	Shaking         bool
	ClickDirection  int
	nearby          []BellEntity
	searched        bool
	Resonating      bool
	ResonationTicks int
}

// OnHit is BellBlockEntity.onHit followed by the block event it sends (triggerEvent with b0 1), which the server
// handles as well: the bell rings.
func (b *Bell) OnHit(l BellLevel, direction int) {
	b.ClickDirection = direction
	if b.Shaking {
		b.Ticks = 0
	} else {
		b.Shaking = true
	}
	b.TriggerEvent(l, direction)
}

// TriggerEvent is BellBlockEntity.triggerEvent for event 1.
func (b *Bell) TriggerEvent(l BellLevel, direction int) {
	b.updateEntities(l)
	b.ResonationTicks = 0
	b.ClickDirection = direction
	b.Ticks = 0
	b.Shaking = true
}

// updateEntities is BellBlockEntity.updateEntities.
func (b *Bell) updateEntities(l BellLevel) {
	now := l.GameTime()
	if now > b.lastRing+bellMinTicksBetweenSearch || !b.searched {
		b.lastRing = now
		b.searched = true
		p := b.Pos.Vec3()
		box := cube.Box(p[0], p[1], p[2], p[0]+1, p[1]+1, p[2]+1).Grow(bellSearchRadius)
		b.nearby = l.LivingEntitiesWithin(box)
	}
	for _, e := range b.nearby {
		if e.Alive() && !e.Removed() && b.closerToCenterThan(e.Position(), bellHearRadius) {
			e.HearBell(now)
		}
	}
	b.nearby = slices.DeleteFunc(b.nearby, func(e BellEntity) bool { return !e.Alive() })
}

// Tick is BellBlockEntity.serverTick.
func (b *Bell) Tick(l BellLevel) {
	if b.Shaking {
		b.Ticks++
	}
	if b.Ticks >= bellDuration {
		b.Shaking = false
		if !b.Resonating {
			b.nearby = b.nearby[:0]
		}
		b.Ticks = 0
	}
	if b.Ticks >= bellTicksBeforeResonation && b.ResonationTicks == 0 && b.raidersNearby() {
		b.Resonating = true
		l.PlayResonate(b.Pos)
	}
	if b.Resonating {
		if b.ResonationTicks < bellMaxResonationTicks {
			b.ResonationTicks++
		} else {
			for _, e := range b.nearby {
				if b.raiderWithinRange(e) {
					e.Glow(BellGlowTicks)
				}
			}
			b.nearby = b.nearby[:0]
			b.Resonating = false
		}
	}
}

// Active reports whether the bell needs ticking.
func (b *Bell) Active() bool { return b.Shaking || b.Resonating }

func (b *Bell) raidersNearby() bool {
	for _, e := range b.nearby {
		if e.Alive() && !e.Removed() && b.closerToCenterThan(e.Position(), bellHearRadius) && e.IsRaider() {
			return true
		}
	}
	return false
}

func (b *Bell) raiderWithinRange(e BellEntity) bool {
	return e.Alive() && !e.Removed() && b.closerToCenterThan(e.Position(), bellHighlightRadius) && e.IsRaider()
}

// closerToCenterThan is Vec3i.closerToCenterThan: the distance from the block's centre is below d.
func (b *Bell) closerToCenterThan(p mgl64.Vec3, d float64) bool {
	return b.Pos.Vec3Centre().Sub(p).LenSqr() < d*d
}
