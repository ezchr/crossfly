//go:build crossfly_full

// Package jraider runs raids with Java mob mode (mobs=java): the raider mobs (pillager, vindicator, evoker with its
// vexes and fangs, ravager, witch) on the Java mob base, and the level side of package jraid: raids ticking per
// world, bad omen and raid omen on players, pillager patrols and bells. The illusioner has no Bedrock entity type
// and is not added.
package jraider

import (
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/component/village"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javaspawn"
	"github.com/df-mc/dragonfly/server/mob/jbrain"
	"github.com/df-mc/dragonfly/server/mob/jbrain/jmob"
	"github.com/df-mc/dragonfly/server/mob/jraid"
	"github.com/df-mc/dragonfly/server/mob/jvillage"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

var install sync.Once

// Install turns on raids for Java mob mode. The server calls it once after javamob.Install, before any world ticks
// (full.go); nothing of this package runs in a world until then.
func Install() {
	install.Do(func() {
		item.RegisterOminousBottle()
		world.RegisterTickHook(worldTick)
		jvillage.RaidHook = raidStatus
		block.BellRingHook = bellRang
	})
}

// worldTick is the raid part of ServerLevel.tick: Raids.tick, the omen effects of the players (BadOmenMobEffect and
// RaidOmenMobEffect.applyEffectTick), ringing bells and the patrol spawner (with natural spawning on, overworld).
func worldTick(tx *world.Tx) {
	l := levelOf(tx)
	st := l.st
	st.raids.Tick(l)
	tickOmens(l)
	tickBells(l)
	w := tx.World()
	if w.Dimension() == world.Overworld && javaspawn.Enabled(w) {
		st.patrol.Tick(l, st.rand, w.Difficulty() != world.DifficultyPeaceful)
	}
	if tx.CurrentTick()%200 == 0 {
		for id, r := range st.refs {
			if r.Removed() {
				delete(st.refs, id)
			}
		}
	}
}

func tickOmens(l level) {
	rs := l.st.raids
	seen := map[uuid.UUID]bool{}
	for e := range l.tx.Players() {
		p, ok := e.(*player.Player)
		if !ok {
			continue
		}
		ref := playerRef{h: p.H(), st: l.st}
		id := p.H().UUID()
		seen[id] = true
		if bad, ok := omen(p, village.BadOmen); ok {
			// On false, AddRaidOmen already replaced bad omen (see playerRef.AddRaidOmen).
			rs.BadOmenTick(l, ref, bad.Level()-1)
		}
		ro, ok := omen(p, village.RaidOmen)
		switch {
		case ok:
			l.st.omen[id] = true
			left := int(ro.Duration() / (time.Second / 20))
			if !rs.RaidOmenTick(l, ref, max(left, 1)) {
				p.RemoveEffect(village.RaidOmen)
				delete(l.st.omen, id)
			}
		case l.st.omen[id]:
			// Raid omen ran out between two world ticks: that was its last tick.
			delete(l.st.omen, id)
			rs.RaidOmenTick(l, ref, 1)
		}
	}
	for id := range l.st.omen {
		if !seen[id] {
			delete(l.st.omen, id)
		}
	}
}

// raidStatus is what villagers read of the raid at a position (ServerLevel.getRaidAt).
func raidStatus(tx *world.Tx, pos cube.Pos) jvillage.RaidStatus {
	r := stateOf(tx.World()).raids.RaidAt(pos)
	if r == nil {
		return jvillage.RaidStatus{}
	}
	return jvillage.RaidStatus{Exists: true, Active: r.IsActive(), Over: r.IsOver(), Victory: r.IsVictory(), Loss: r.IsLoss(),
		Stopped: r.IsStopped(), FirstWaveSpawned: r.HasFirstWaveSpawned(), BetweenWaves: r.IsBetweenWaves()}
}

// bellRang is BellBlockEntity.onHit: villagers within 32 blocks hear it; with a raider near, the bell resonates
// and raiders within 48 blocks glow.
func bellRang(tx *world.Tx, pos cube.Pos, face cube.Face) {
	l := levelOf(tx)
	b, ok := l.st.bells[pos]
	if !ok {
		b = &jraid.Bell{Pos: pos}
		l.st.bells[pos] = b
	}
	b.OnHit(l, int(face))
}

func tickBells(l level) {
	for pos, b := range l.st.bells {
		if _, ok := l.tx.Block(pos).(block.Bell); !ok {
			delete(l.st.bells, pos)
			continue
		}
		b.Tick(l)
		if !b.Active() {
			delete(l.st.bells, pos)
		}
	}
}

// hearBell sets HEARD_BELL_TIME of a Java villager.
func hearBell(e world.Entity, gameTime int64) {
	if e.H().Type().EncodeEntity() != jvillage.VillagerID {
		return
	}
	m, ok := javamob.Of(e)
	if !ok {
		return
	}
	if mem := jmob.Mem(m); mem != nil {
		jbrain.Set(mem, jbrain.MemHeardBellTime, gameTime)
	}
}

// spawnPatrolMember is PatrolSpawner.spawnPatrolMember: a pillager for a patrol, the leader with a patrol target.
func spawnPatrolMember(l level, pos cube.Pos, leader bool) bool {
	_, err := javamob.Spawn(l.tx, PillagerID, pos.Vec3(), javamob.SpawnPatrol, javamob.SpawnOptions{Configure: func(m *javamob.Mob) {
		rb, ok := m.Behaviour().(raiderBehaviour)
		if !ok || !leader {
			return
		}
		r := rb.raider()
		r.SetPatrolLeader(true)
		r.FindPatrolTarget(m.Rand(), pos)
	}})
	return err == nil
}
