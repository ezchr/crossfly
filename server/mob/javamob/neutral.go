//go:build crossfly_full

package javamob

import (
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// NeutralMob is Java's NeutralMob: persistent anger at one entity for a while (endermen, zombified piglins, wolves,
// bees, polar bears, llamas, iron golems). Embed it in a behaviour, set AngerTime, call UpdatePersistentAnger from
// the behaviour's AiStep or CustomServerAiStep like the Java class does, and save it from the behaviour's
// Persister (EncodeAnger, DecodeAnger). The game rules universalAnger (false) and forgiveDeadPlayers (true) are at
// their defaults.
type NeutralMob struct {
	// AngerTime is the range of anger, in ticks, startPersistentAngerTimer picks from (TimeUtil.rangeOfSeconds).
	AngerTime [2]int
	// endTick is the anger end in world ticks (-1: not angry); target the entity the mob is angry at.
	endTick int64
	target  *world.EntityHandle
	// targetID is the target's UUID when it was loaded from NBT and is not in the world yet.
	targetID uuid.UUID
	// lastNow is the world tick the anger was last looked at (EncodeAnger runs outside any transaction).
	lastNow int64
}

func (n *NeutralMob) init() {
	if n.endTick == 0 {
		n.endTick = -1
	}
}

// StartPersistentAngerTimer is startPersistentAngerTimer: angry for a random time of AngerTime.
func (n *NeutralMob) StartPersistentAngerTimer(m *Mob) {
	lo, hi := n.AngerTime[0], n.AngerTime[1]
	d := lo
	if hi > lo {
		d += m.Rand().IntN(hi - lo + 1)
	}
	n.endTick = m.Tx().CurrentTick() + int64(d)
}

// SetTimeToRemainAngry is setTimeToRemainAngry.
func (n *NeutralMob) SetTimeToRemainAngry(m *Mob, ticks int) {
	n.endTick = m.Tx().CurrentTick() + int64(ticks)
}

// IsAngry is isAngry: the anger time has not run out.
func (n *NeutralMob) IsAngry(m *Mob) bool {
	n.lastNow = m.Tx().CurrentTick()
	n.init()
	return n.endTick > 0 && n.endTick-m.Tx().CurrentTick() > 0
}

// AngerTarget returns the entity the mob is angry at, if it is in the world.
func (n *NeutralMob) AngerTarget(m *Mob) (world.Entity, bool) {
	if n.target == nil {
		if n.targetID == uuid.Nil {
			return nil, false
		}
		for e := range m.Tx().Entities() {
			if e.H().UUID() == n.targetID {
				n.target = e.H()
				break
			}
		}
		if n.target == nil {
			return nil, false
		}
	}
	return n.target.Entity(m.Tx())
}

func (n *NeutralMob) matches(e world.Entity) bool {
	if e == nil {
		return false
	}
	if n.target != nil {
		return n.target == e.H()
	}
	return n.targetID != uuid.Nil && n.targetID == e.H().UUID()
}

func (n *NeutralMob) hasTarget() bool { return n.target != nil || n.targetID != uuid.Nil }

func (n *NeutralMob) setTarget(e world.Entity) {
	n.target, n.targetID = nil, uuid.Nil
	if e != nil {
		n.target, n.targetID = e.H(), e.H().UUID()
	}
}

// IsAngryAt is isAngryAt.
func (n *NeutralMob) IsAngryAt(m *Mob, e world.Entity) bool {
	return m.validTarget(e) && n.matches(e)
}

// UpdatePersistentAnger is updatePersistentAnger.
func (n *NeutralMob) UpdatePersistentAnger(m *Mob, stayAngryIfTargetPresent bool) {
	n.init()
	prev, hadPrev := m.Mob.Target()
	if hadPrev && n.hasTarget() && n.matches(prev) {
		if l, ok := prev.(entity.Living); ok && l.Dead() {
			if _, isMob := mob.Of(prev); isMob {
				n.StopBeingAngry(m)
				return
			}
		}
	}
	target, ok := m.Target()
	if ok {
		newTarget := !n.hasTarget() || !n.matches(target)
		if newTarget {
			n.setTarget(target)
		}
		if newTarget || stayAngryIfTargetPresent {
			n.StartPersistentAngerTimer(m)
		}
	}
	if n.hasTarget() && !n.IsAngry(m) && (!ok || !validPlayerTarget(m, target) || !stayAngryIfTargetPresent) {
		n.StopBeingAngry(m)
	}
	if e, ok := n.AngerTarget(m); ok {
		if p, isPlayer := e.(*player.Player); isPlayer && (!p.GameMode().AllowsTakingDamage() || !p.GameMode().Visible() || m.DifficultyID() == 0) {
			n.StopBeingAngry(m)
		}
	}
}

func validPlayerTarget(m *Mob, e world.Entity) bool {
	p, ok := e.(*player.Player)
	return ok && p.GameMode().AllowsTakingDamage() && p.GameMode().Visible() && m.DifficultyID() != 0
}

// StopBeingAngry is stopBeingAngry.
func (n *NeutralMob) StopBeingAngry(m *Mob) {
	m.SetLastHurtBy(nil)
	n.setTarget(nil)
	m.SetTarget(nil)
	n.endTick = -1
}

// PlayerDied is playerDied (forgiveDeadPlayers): a dead player is forgiven.
func (n *NeutralMob) PlayerDied(m *Mob, p *player.Player) {
	if n.matches(p) {
		n.StopBeingAngry(m)
	}
}

// EncodeAnger saves the anger (addPersistentAngerSaveData) as the time left, since world ticks restart with the
// server.
func (n *NeutralMob) EncodeAnger(data map[string]any) {
	now := n.lastNow
	n.init()
	left := int64(-1)
	if n.endTick > 0 {
		left = n.endTick - now
	}
	data["anger_time_left"] = left
	if n.targetID != uuid.Nil {
		data["angry_at"] = n.targetID.String()
	}
}

// DecodeAnger loads the anger (readPersistentAngerSaveData).
func (n *NeutralMob) DecodeAnger(m *Mob, data map[string]any) {
	n.endTick = -1
	if left, ok := data["anger_time_left"].(int64); ok && left > 0 {
		n.endTick = m.Tx().CurrentTick() + left
	}
	if s, ok := data["angry_at"].(string); ok {
		if id, err := uuid.Parse(s); err == nil {
			n.targetID = id
		}
	}
}
