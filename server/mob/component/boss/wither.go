//go:build crossfly_full

package boss

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// WitherID is the wither's identifier.
const WitherID = "minecraft:wither"

// Wither constants (WitherBoss).
const (
	// WitherSpawnFrames is the spawn charge: invulnerable, healing, then
	// an explosion of power 7.
	WitherSpawnFrames = 200
	// witherDeathFrames is the death animation before the final explosion.
	witherDeathFrames = 200
	// witherHeadRange is the side heads' target range (30).
	witherHeadRange = 30
	// witherPause is how long the wither freezes after its phase-2 landing
	// or a hit by an ownerless skull.
	witherPause = 80
	// witherDashTicks / witherWindup are the phase-2 dash.
	witherDashTicks = 10
	witherWindup    = 20
)

type witherKey struct{}

// witherHead is one side head (WitherBoss heads 1 and 2).
type witherHead struct {
	target *world.EntityHandle
	next   int64
	idle   int
}

// Wither is the state BDS keeps on the WitherBoss actor: the spawn charge,
// the phase, the three heads, the attack timers and the dash. The systems
// "boss/wither/*" run it (WitherBossPreAIStepSystem, newServerAiStep,
// postAiStep), the boss component its damage rules.
type Wither struct {
	SpawnFrames int
	// Phase1 is AerialAttack (actor data 52): true until half health, when
	// the armour shows and projectiles stop hurting it.
	Phase1 bool
	heads  [2]witherHead

	mainTimer, fireRate int
	shots               int
	actionCountdown     int
	ShotDelay           int
	pause               int
	cooldown            int
	toggle              bool
	landing             bool
	dashTicks, windup   int
	dashDir             mgl64.Vec3
	// WantsToMove / Pathing: the relocation phase-1 uses
	// (WitherRandomAttackPosGoal) and its wanted position.
	WantsToMove bool
	Pathing     bool
	PathTarget  mgl64.Vec3
	destroyTick int
	idle        int
	lastHealth  float64
	dying       int
	flash       int
	spin        float64

	fresh     bool
	decoded   bool
	frozen    bool
	noGravity bool
	// Damage is the damage each player dealt (BossComponent party, read by
	// wither_target_highest_damage).
	Damage map[*world.EntityHandle]float64
	death  world.DamageSource
}

// WitherOf returns m's wither state (made on first use).
func WitherOf(m *mob.Mob) *Wither {
	if v, ok := m.Data(witherKey{}); ok {
		return v.(*Wither)
	}
	w := &Wither{Phase1: true, fireRate: 20, mainTimer: 20, cooldown: 40, Damage: map[*world.EntityHandle]float64{}}
	m.SetData(witherKey{}, w)
	return w
}

func (w *Wither) attach(*mob.Mob) { w.fresh = true }

// CanShoot is WitherBoss::canShoot.
func (w *Wither) CanShoot() bool {
	return w.actionCountdown == 0 && !w.WantsToMove && w.dashTicks == 0 && w.windup == 0 && w.ShotDelay <= 0
}

// Invulnerable is the spawn charge (or the death animation).
func (w *Wither) Invulnerable() bool { return w.SpawnFrames > 0 || w.dying > 0 }

// Dying reports whether the death animation runs.
func (w *Wither) Dying() bool { return w.dying > 0 }

// Dashing reports whether a phase-2 dash (or its wind-up) runs.
func (w *Wither) Dashing() bool { return w.dashTicks > 0 || w.windup > 0 }

// Paused reports whether the wither is frozen (after landing or a hit).
func (w *Wither) Paused() bool { return w.pause > 0 }

// HeadTarget is the target of side head i (1 or 2).
func (w *Wither) HeadTarget(i int) *world.EntityHandle { return w.heads[i-1].target }

// FireRate is the main head's shot interval in ticks.
func (w *Wither) FireRate() int { return w.fireRate }

// HeadPos is WitherBoss::getHeadPos: the middle head 3 above the feet, the
// side ones 1.3 to the sides and 2.2 up. source: java/paper for the side
// heads' angle (the decompile lost it).
func HeadPos(m *mob.Mob, head int) mgl64.Vec3 {
	p := m.Position()
	if head == 0 {
		return p.Add(mgl64.Vec3{0, 3, 0})
	}
	a := m.BodyYaw() + 180*float64(head-1)
	return p.Add(mgl64.Vec3{cosDeg(a) * 1.3, 2.2, sinDeg(a) * 1.3})
}

// syncData shows the wither's actor data: the head targets (49-51), the
// invulnerable ticks (48), aerial attack (52) and the spawn frames (105).
func (w *Wither) syncData(m *mob.Mob) {
	m.SetActorData(protocol.EntityDataKeyTargetA, m.TargetHandle())
	m.SetActorData(protocol.EntityDataKeyTargetB, w.heads[0].target)
	m.SetActorData(protocol.EntityDataKeyTargetC, w.heads[1].target)
	inv := int32(w.SpawnFrames)
	if w.dying > 0 {
		inv = int32(w.dying)
	}
	m.SetActorData(protocol.EntityDataKeyInvulnerableTicks, inv)
	var aerial int16
	if w.Phase1 {
		aerial = 1
	}
	if w.dying > 0 && w.flash%2 == 1 {
		aerial = 1 - aerial
	}
	m.SetActorData(protocol.EntityDataKeyAerialAttack, aerial)
	m.SetActorData(protocol.EntityDataKeySpawningFrames, int32(w.SpawnFrames))
}

// spawnInit is WitherBoss::reloadHardcoded for a new wither: health by
// difficulty (300/450/600 of 600), the 200-tick charge starting at 1 health.
func (w *Wither) spawnInit(m *mob.Mob) {
	max := m.MaxHealth()
	switch sense.DifficultyID(m.Tx()) {
	case 1:
		max *= 0.5
	case 2:
		max *= 0.75
	}
	m.SetMaxHealth(max)
	m.SetHealth(1)
	w.SpawnFrames = WitherSpawnFrames
	w.lastHealth = max
	w.Phase1 = true
	m.SetPersistent(true)
}

// witherPre is WitherBossPreAIStepSystem: the death animation, the spawn
// charge and the pause freeze the wither (no AI, no movement); otherwise
// the timers count down and it flies (newServerAiStep).
func witherPre(m *mob.Mob) {
	if m.Identifier() != WitherID {
		return
	}
	w := WitherOf(m)
	if w.fresh {
		w.fresh = false
		if !w.decoded {
			w.spawnInit(m)
		} else if m.Health() <= m.MaxHealth()/2 {
			w.Phase1 = false
		}
	}
	defer w.syncData(m)
	w.frozen = true
	if w.dying > 0 {
		w.tickDeath(m)
		return
	}
	if w.SpawnFrames > 0 {
		w.tickSpawn(m)
		return
	}
	if w.ShotDelay > 0 {
		w.ShotDelay--
	}
	if w.pause > 0 {
		w.pause--
		return
	}
	w.frozen = false
	w.serverStep(m)
}

// witherFreeze runs after the controls: while the wither charges, dies or
// pauses (preAiStep result 0) it neither moves nor falls, and during the
// charge it has no target.
func witherFreeze(m *mob.Mob) {
	if m.Identifier() != WitherID {
		return
	}
	w := WitherOf(m)
	if !w.frozen {
		if w.noGravity {
			w.noGravity = false
			m.UpdateMeta(func(meta *mob.Meta) { meta.NoGravity = false })
		}
		return
	}
	m.StopMovement()
	m.SetVelocity(mgl64.Vec3{})
	if w.SpawnFrames > 0 || w.dying > 0 {
		m.ResetTarget()
	}
	if !m.Meta().NoGravity {
		w.noGravity = true
		m.UpdateMeta(func(meta *mob.Meta) { meta.NoGravity = true })
	}
}

// tickSpawn is the spawn charge: it heals ceil(max/200) a tick and spins,
// then explodes with power 7 (blocks with mobGriefing).
func (w *Wither) tickSpawn(m *mob.Mob) {
	w.SpawnFrames--
	m.Heal(math.Ceil(m.MaxHealth()*0.005), nil)
	w.spin += 0.2
	yaw := wrapDegrees(m.BodyYaw() - (10 + w.spin))
	m.SetYaw(yaw)
	m.SetBodyYaw(yaw)
	m.SetHeadYaw(yaw)
	if w.SpawnFrames == 0 {
		Explode(m.Tx(), m, m.EyePosition(), 7, false, MobGriefing(m.Tx()))
		m.PlaySound("spawn")
	}
}

// Explode is an explosion of power at pos caused by src: blocks break when
// blocks is set (Dragonfly cannot spare them otherwise). Players a wither
// or its skull kill with it leave a wither rose.
func Explode(tx *world.Tx, src world.Entity, pos mgl64.Vec3, power float64, fire, blocks bool) {
	var victims []world.Entity
	if src != nil && isWitherOrSkull(src) {
		r := power*2 + 1
		victims = livePlayersNear(tx, cube.Box(pos[0]-r, pos[1]-r, pos[2]-r, pos[0]+r, pos[1]+r, pos[2]+r))
	}
	if blocks {
		block.ExplosionConfig{SpawnFire: fire, Source: src}.Explode(tx, explosionAt{src: src, pos: pos, size: power})
	} else {
		combat.ExplodeEntitiesOnly(tx, explosionEntity{Entity: src, pos: pos}, power)
	}
	plantForKilledPlayers(tx, victims)
}

// explosionAt is an entity explosion at a position other than the entity's.
type explosionAt struct {
	src  world.Entity
	pos  mgl64.Vec3
	size float64
}

func (e explosionAt) Position() mgl64.Vec3 { return e.pos }
func (e explosionAt) Size() float64        { return e.size }
func (e explosionAt) Entity() world.Entity { return e.src }

// explosionEntity moves an entity to pos for combat.ExplodeEntitiesOnly.
type explosionEntity struct {
	world.Entity
	pos mgl64.Vec3
}

func (e explosionEntity) Position() mgl64.Vec3 { return e.pos }

// serverStep is WitherBoss::newServerAiStep/aiStep: flying above (phase 1)
// or level with (phase 2) the target, the dash, the heads and their skulls,
// block breaking and regeneration.
func (w *Wither) serverStep(m *mob.Mob) {
	tx := m.Tx()
	vel := m.Velocity()
	vel[1] *= 0.6
	target, hasTarget := m.Target()
	if hasTarget && !Living(target) {
		hasTarget = false
	}
	if hasTarget {
		ty := target.Position()[1]
		y := m.Position()[1]
		switch {
		case w.Pathing:
			vel[1] = clamp(w.PathTarget[1]-y, -0.5, 0.5)
		case (ty <= y || w.landing) && (!w.Phase1 || ty+5 <= y):
			if w.landing {
				vel[1] -= 1
			}
		default:
			v := math.Max(vel[1], 0)
			vel[1] = v + (0.5-v)*0.6
		}
		// source: java/paper WitherBoss.aiStep: the horizontal approach
		// (BDS flies with its walk navigation, which cannot path in the air).
		goal := target.Position()
		if w.Pathing {
			goal = w.PathTarget
		}
		to := goal.Sub(m.Position())
		to[1] = 0
		if to.LenSqr() > 9 && w.dashTicks == 0 {
			n := to.Normalize()
			vel[0] += n[0]*0.3 - vel[0]*0.6
			vel[2] += n[2]*0.3 - vel[2]*0.6
		}
	}
	// Phase-2 landing: explode, pause, wither skeletons.
	if w.landing && (m.OnGround() || m.Physics().InWater) {
		w.landing = false
		Explode(tx, m, HeadPos(m, 0), 7, false, MobGriefing(tx))
		w.pause = witherPause
		if sense.DifficultyID(tx) >= 2 {
			for range 3 {
				if _, err := mob.Spawn(tx, "minecraft:wither_skeleton", m.Position(), mob.SpawnOptions{}); err != nil {
					break
				}
			}
		}
	}
	// The dash.
	if w.windup > 0 {
		if w.windup--; w.windup == 0 && hasTarget {
			w.dashTicks = witherDashTicks
		}
	}
	if w.dashTicks > 0 {
		vel[0], vel[2] = w.dashDir[0]*2, w.dashDir[2]*2
		w.destroyBlocks(m, 2, false)
		w.dashHit(m)
		if w.dashTicks--; w.dashTicks == 0 {
			w.dashDir = mgl64.Vec3{}
			w.ShotDelay = 20
			vel[0], vel[2] = 0, 0
		}
	}
	m.SetVelocity(vel)

	// Body and head yaw follow the target.
	if hasTarget {
		yaw := sense.YawTowards(HeadPos(m, 0), sense.EyePos(target))
		m.SetHeadYaw(rotateTowards(m.HeadYaw(), yaw, 10))
		m.SetBodyYaw(rotateTowards(m.BodyYaw(), m.HeadYaw(), 45))
		m.SetYaw(m.BodyYaw())
	}

	// Action countdown: relocate (phase 1) or dash (phase 2).
	if w.actionCountdown > 0 {
		if w.actionCountdown--; w.actionCountdown == 0 {
			if w.Phase1 {
				w.WantsToMove = true
			} else if hasTarget {
				w.windup = witherWindup
				d := target.Position().Sub(m.Position())
				if d.Len() > 0 {
					w.dashDir = d.Normalize()
				}
			}
		}
	}
	if w.CanShoot() && hasTarget {
		if w.idle++; w.idle > 100 {
			w.actionCountdown, w.idle = 1, 0
		}
	}

	// Main head.
	if w.mainTimer--; w.mainTimer <= 0 {
		if w.CanShoot() && hasTarget {
			m.Navigation().Stop(m)
			w.shots++
			w.idle = 0
			dangerous := w.shots%4 == 0
			ShootWitherSkull(m, 0, sense.EyePos(target), dangerous)
			if dangerous {
				w.actionCountdown = w.cooldown
				if !w.Phase1 {
					if w.toggle = !w.toggle; w.toggle {
						w.actionCountdown, w.ShotDelay = 0, 60
					}
				}
			}
		}
		w.mainTimer = w.fireRate
	}

	// Side heads.
	normal := sense.DifficultyID(tx) >= 2
	for i := range w.heads {
		h := &w.heads[i]
		if m.Ticks() < h.next || !w.CanShoot() {
			continue
		}
		h.next = m.Ticks() + 10 + int64(m.Rand().IntN(10))
		if normal {
			if h.idle++; h.idle > 15 {
				r := m.Rand()
				p := m.Position().Add(mgl64.Vec3{-10 + r.Float64()*20, -5 + r.Float64()*10, -10 + r.Float64()*20})
				if i == 0 {
					ShootWitherSkull(m, 1, p, true)
				}
				h.idle = 0
			}
		}
		if h.target == nil {
			w.pickHeadTarget(m, i)
			continue
		}
		t, ok := h.target.Entity(tx)
		if !ok || !Living(t) || t.Position().Sub(m.Position()).LenSqr() > witherHeadRange*witherHeadRange ||
			!sense.CanSee(m, t) || sense.Invulnerable(t) {
			h.target = nil
			continue
		}
		if normal {
			ShootWitherSkull(m, i+1, sense.EyePos(t), false)
			h.next = m.Ticks() + 40 + int64(m.Rand().IntN(20))
			h.idle = 0
		}
	}

	// Blocks broken after a hit.
	if w.destroyTick > 0 {
		if w.destroyTick--; w.destroyTick == 0 {
			w.destroyBlocks(m, 1, true)
		}
	}
	// Regeneration without players around.
	if m.Ticks()%20 == 0 {
		alone := true
		for e := range tx.EntitiesWithin(m.BBox().Grow(50)) {
			if sense.IsPlayer(e) {
				alone = false
				break
			}
		}
		if alone {
			m.Heal(1, nil)
		}
	}
	w.particles(m)
}

// pickHeadTarget gives side head i a random player within (30, 8, 30) that
// no other head targets.
func (w *Wither) pickHeadTarget(m *mob.Mob, i int) {
	tx := m.Tx()
	var cands []world.Entity
	for e := range tx.EntitiesWithin(m.BBox().GrowVec3(mgl64.Vec3{30, 8, 30})) {
		if !AttackablePlayer(e) || e.H() == m.TargetHandle() || e.H() == w.heads[1-i].target {
			continue
		}
		cands = append(cands, e)
	}
	for range 10 {
		if len(cands) == 0 {
			return
		}
		w.heads[i].target = cands[m.Rand().IntN(len(cands))].H()
		return
	}
}

// particles shows the smoke at the heads.
func (w *Wither) particles(m *mob.Mob) {
	if m.Ticks()%4 != 0 {
		return
	}
	for i := range 3 {
		m.Tx().AddParticle(HeadPos(m, i), particle.Named{Bedrock: "minecraft:basic_smoke_particle", Java: "smoke"})
	}
}

// rotateTowards turns a towards b by at most step degrees.
func rotateTowards(a, b, step float64) float64 {
	return a + clamp(wrapDegrees(b-a), -step, step)
}

// dashHit is the dash damage: 15 to every mob and player in the box grown
// (1, 4, 1).
func (w *Wither) dashHit(m *mob.Mob) {
	var hit []world.Entity
	for e := range m.Tx().EntitiesWithin(m.BBox().GrowVec3(mgl64.Vec3{1, 4, 1})) {
		if e.H() != m.H() && IsLiving(e) && Living(e) && !sense.Invulnerable(e) {
			hit = append(hit, e)
		}
	}
	for _, e := range hit {
		if l, ok := e.(entity.Living); ok {
			l.Hurt(15, entity.AttackDamageSource{Attacker: m})
		}
	}
	plantForKilledPlayers(m.Tx(), hit)
}

// witherImmune are the blocks the wither never breaks (WitherBoss::canDestroy).
var witherImmune = map[string]bool{
	"minecraft:bedrock": true, "minecraft:end_portal": true, "minecraft:end_portal_frame": true,
	"minecraft:command_block": true, "minecraft:repeating_command_block": true, "minecraft:chain_command_block": true,
	"minecraft:barrier": true, "minecraft:invisible_bedrock": true, "minecraft:border_block": true,
	"minecraft:allow": true, "minecraft:deny": true, "minecraft:chalkboard": true, "minecraft:camera": true,
	"minecraft:structure_block": true, "minecraft:structure_void": true, "minecraft:reinforced_deepslate": true,
}

// CanDestroy is WitherBoss::canDestroy: obsidian only with obsidian set
// (hits and skulls, not the dash).
func CanDestroy(b world.Block, obsidian bool) bool {
	name := blockName(b)
	if witherImmune[name] {
		return false
	}
	if !obsidian && (name == "minecraft:obsidian" || name == "minecraft:crying_obsidian") {
		return false
	}
	return true
}

// destroyBlocks is WitherBoss::_destroyBlocks: every breakable block in the
// box grown by r drops and breaks (mobGriefing).
func (w *Wither) destroyBlocks(m *mob.Mob, r float64, obsidian bool) {
	tx := m.Tx()
	if !MobGriefing(tx) {
		return
	}
	broke := false
	boxBlocks(m.BBox().Grow(r), func(p cube.Pos) {
		b := tx.Block(p)
		if _, ok := b.(block.Air); ok {
			return
		}
		if _, ok := b.(world.Liquid); ok || !CanDestroy(b, obsidian) {
			return
		}
		if br, ok := b.(block.Breakable); ok {
			for _, drop := range br.BreakInfo().Drops(nil, nil) {
				it := entity.NewItem(world.EntitySpawnOpts{Position: p.Vec3Centre()}, drop)
				tx.AddEntity(it)
			}
		}
		tx.SetBlock(p, nil, nil)
		broke = true
	})
	if broke {
		m.PlaySound("break_block")
	}
}

// explosionCauser is the entity that caused an explosion's damage.
func explosionCauser(src world.DamageSource) world.Entity {
	ex, ok := src.(entity.ExplosionDamageSource)
	if !ok {
		return nil
	}
	var e world.Entity
	switch s := ex.Source.(type) {
	case explosionAt:
		e = s.src
	case world.EntityExplosionSource:
		e = s.Entity
	default:
		if sb, ok := ex.Source.(interface {
			ShieldBlockInfo() (world.ShieldBlockInfo, bool)
		}); ok {
			info, _ := sb.ShieldBlockInfo()
			e = info.Source
		}
	}
	if w, ok := e.(explosionEntity); ok {
		e = w.Entity
	}
	return e
}

// isWitherDamage reports whether d comes from a wither or its skulls.
func isWitherDamage(d *mob.Damage) bool {
	if d.Attacker != nil && d.Attacker.H().Type().EncodeEntity() == WitherID {
		return true
	}
	if e := explosionCauser(d.Source); e != nil {
		return e.H().Type().EncodeEntity() == WitherID || skullOwnedByWither(e)
	}
	return false
}

// modifyDamage is WitherBoss::_hurt: nothing hurts it during the charge or
// the death animation (but the void), nor drowning, falls, withers and
// their skulls; whoever melees it during the charge is knocked back;
// projectiles bounce off the armour (phase 2). A lethal hit starts the death
// animation.
func (w *Wither) modifyDamage(m *mob.Mob, d *mob.Damage) {
	_, void := d.Source.(entity.VoidDamageSource)
	switch d.Source.(type) {
	case entity.FallDamageSource, entity.DrowningDamageSource:
		d.Cancel = true
		return
	}
	if isWitherDamage(d) {
		d.Cancel = true
		return
	}
	if a, ok := d.Source.(entity.AttackDamageSource); ok && w.SpawnFrames > 0 && a.Attacker != nil && IsLiving(a.Attacker) {
		// A mob or player hitting the charging wither is knocked away from
		// it (Mob::knockback with its default power and height, 0.4).
		if l, ok := a.Attacker.(entity.Living); ok {
			l.KnockBack(m.Position(), 0.4, 0.4)
		}
	}
	if w.Invulnerable() && !void {
		d.Cancel = true
		return
	}
	if p, ok := d.Source.(entity.ProjectileDamageSource); ok {
		if !w.Phase1 {
			d.Cancel = true
			return
		}
		if p.Owner == nil && p.Projectile != nil && p.Projectile.H().Type() == WitherSkullType {
			w.pause = witherPause
		}
	}
	if !void && d.Amount >= m.Health() {
		d.Amount = math.Max(m.Health()-1, 0)
		w.dying = witherDeathFrames
		w.flash = 15
		w.death = d.Source
		m.PlaySound("death.min.volume")
	}
}

// onHurt is WitherBoss::hurtEffects: blocks break 20 ticks after a hit,
// the fire rate halves every sixth of health lost (not on easy), the
// armour comes at half health (with a dive to the ground), and the damage
// a player dealt is counted.
func (w *Wither) onHurt(m *mob.Mob, dealt float64, src world.DamageSource) {
	if w.dying > 0 {
		return
	}
	if w.destroyTick <= 0 {
		w.destroyTick = 20
	}
	for i := range w.heads {
		w.heads[i].idle += 3
	}
	w.idle += 3
	attacker := attackerOf(src)
	if attacker != nil && sense.IsPlayer(attacker) {
		w.Damage[attacker.H()] += dealt
	}
	max := m.MaxHealth()
	if sense.DifficultyID(m.Tx()) >= 2 && m.Health() < w.lastHealth-max/6 {
		w.lastHealth -= max / 6
		w.fireRate = int(math.Max(float64(w.fireRate)*0.5, 1))
	}
	if w.Phase1 && m.Health() <= max/2 {
		w.Phase1 = false
		w.landing = true
		w.cooldown *= 2
		w.WantsToMove = false
		w.fireRate = 20
	}
	if attacker != nil && attacker.H() == m.TargetHandle() {
		if !sense.CanSee(m, attacker) || attacker.Position().Sub(m.Position()).LenSqr() > 900 {
			w.actionCountdown = 1
		}
	}
}

// attackerOf is the entity responsible for src.
func attackerOf(src world.DamageSource) world.Entity {
	switch s := src.(type) {
	case entity.AttackDamageSource:
		return s.Attacker
	case entity.ProjectileDamageSource:
		if s.Owner != nil {
			return s.Owner
		}
		return s.Projectile
	case SonicBoomDamageSource:
		return s.Attacker
	}
	return nil
}

// tickDeath is the death animation: the armour flashes ever faster, sounds
// at 120 and 60 frames left, then an explosion of power 7 and the death
// (loot: the nether star, 50 experience).
func (w *Wither) tickDeath(m *mob.Mob) {
	w.dying--
	if w.flash > 0 && w.dying%w.flash == 0 {
		w.flash = max(w.flash-1, 1)
	}
	switch w.dying {
	case 120:
		m.PlaySound(mob.SoundDeath)
	case 60:
		m.PlaySound("death.mid.volume")
	}
	if w.dying <= 0 {
		tx := m.Tx()
		Explode(tx, m, m.EyePosition(), 7, false, MobGriefing(tx))
		src := w.death
		if src == nil {
			src = entity.VoidDamageSource{}
		}
		w.dying = 0
		m.Kill(src)
	}
}

func (w *Wither) onDeath(*mob.Mob) {}

func (w *Wither) encode() map[string]any {
	phase := byte(0)
	if w.Phase1 {
		phase = 1
	}
	return map[string]any{
		"Invul": int32(w.SpawnFrames), "AirAttack": phase, "firerate": int32(w.fireRate),
		"lastHealthInterval": float32(w.lastHealth), "dyingFrames": int32(w.dying),
	}
}

func (w *Wither) decode(m *mob.Mob, data map[string]any) {
	w.decoded = true
	if v, ok := data["Invul"].(int32); ok {
		w.SpawnFrames = int(v)
	}
	if v, ok := data["AirAttack"].(byte); ok {
		w.Phase1 = v == 1
	}
	if v, ok := data["firerate"].(int32); ok && v > 0 {
		w.fireRate = int(v)
	}
	if v, ok := data["lastHealthInterval"].(float32); ok {
		w.lastHealth = float64(v)
	}
	if v, ok := data["dyingFrames"].(int32); ok {
		w.dying = int(v)
	}
	if !w.Phase1 {
		w.cooldown = 80
	}
}
