//go:build crossfly_full

package jnether

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/component/combat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The wither of Java mob mode (WitherBoss, Paper 26.3): three heads, the
// middle one shoots at the mob's target (RangedAttackGoal, 40 ticks, range
// 20), the side heads pick their own targets within 20 (8 up and down) and
// shoot every 40-60 ticks, and on normal and hard shoot blue skulls at random
// spots when idle for a while. It spawns with 220 ticks of invulnerability
// (healing 10 every 10 ticks from a third of its health, then an explosion
// of 7), breaks the blocks around it 20 ticks after being hurt, heals 1 a
// second, and at half health (powered) arrows and wind charges bounce off.

// WitherID is the wither's identifier.
const WitherID = "minecraft:wither"

// witherInvulnerableTicks is WitherBoss.INVULNERABLE_TICKS.
const witherInvulnerableTicks = 220

// WitherHost is what the wither needs of its mob (the javamob base).
type WitherHost interface {
	world.Entity
	Tx() *world.Tx
	Velocity() mgl64.Vec3
	SetVelocity(v mgl64.Vec3)
	Yaw() float64
	SetYaw(v float64)
	BodyYaw() float64
	Health() float64
	MaxHealth() float64
	SetHealth(v float64)
	Rand() *rand.Rand
	TickCount() int
	// Target is the mob's attack target (Mob.getTarget).
	Target() (world.Entity, bool)
}

// Wither is the state of one Java wither (WitherBoss's fields).
type Wither struct {
	// targets are the alternative targets of the three heads (DATA_TARGET_A
	// .. C): head 0 follows the mob's target.
	targets [3]*world.EntityHandle
	// headX/headY are the side heads' rotations (xRotHeads, yRotHeads).
	headX, headY       [2]float64
	nextHeadUpdate     [2]int
	idleHeadUpdates    [2]int
	destroyBlocksTick  int
	invulnerable       int
	bar                bossBar
	explodedAfterSpawn bool
}

// NewWither returns a wither (the bar is purple and darkens the sky).
func NewWither() *Wither {
	return &Wither{bar: bossBar{colour: 5, darken: true}}
}

// MakeInvulnerable is WitherBoss.makeInvulnerable, run when the pattern of
// skulls summons it: 220 ticks of invulnerability from a third of its
// health.
func (w *Wither) MakeInvulnerable(h WitherHost) {
	w.invulnerable = witherInvulnerableTicks
	h.SetHealth(h.MaxHealth() / 3)
}

// InvulnerableTicks is DATA_ID_INV.
func (w *Wither) InvulnerableTicks() int { return w.invulnerable }

// Powered is WitherBoss.isPowered: at half health or less.
func (w *Wither) Powered(h WitherHost) bool { return h.Health() <= h.MaxHealth()/2 }

// HeadTarget is the target of head i (0 the middle one).
func (w *Wither) HeadTarget(i int) *world.EntityHandle { return w.targets[i] }

// AiStep is the part of WitherBoss.aiStep before super.aiStep: the motion
// is damped vertically (0.6); with a target the wither climbs while below
// it (or below 5 above it when not powered) and closes in to 3 blocks
// horizontally; it faces where it moves.
func (w *Wither) AiStep(h WitherHost) {
	tx := h.Tx()
	v := h.Velocity()
	v[1] *= 0.6
	if t := w.targets[0]; t != nil {
		if e, ok := t.Entity(tx); ok {
			pos, tp := h.Position(), e.Position()
			yd := v[1]
			if pos[1] < tp[1] || !w.Powered(h) && pos[1] < tp[1]+5 {
				yd = math.Max(0, yd)
				yd += 0.3 - yd*0.6
			}
			v[1] = yd
			delta := mgl64.Vec3{tp[0] - pos[0], 0, tp[2] - pos[2]}
			if delta[0]*delta[0]+delta[2]*delta[2] > 9 {
				s := normalize(delta)
				v = v.Add(mgl64.Vec3{s[0]*0.3 - v[0]*0.6, 0, s[2]*0.3 - v[2]*0.6})
			}
		}
	}
	h.SetVelocity(v)
	if v[0]*v[0]+v[2]*v[2] > 0.05 {
		h.SetYaw(math.Atan2(v[2], v[0])*180/math.Pi - 90)
	}
}

// PostAiStep is the head turning after super.aiStep: the side heads turn
// to their targets (10 degrees of yaw, 40 of pitch a tick) or back to the
// body.
func (w *Wither) PostAiStep(h WitherHost) {
	tx := h.Tx()
	for i := range 2 {
		var e world.Entity
		if t := w.targets[i+1]; t != nil {
			e, _ = t.Entity(tx)
		}
		if e == nil {
			w.headY[i] = rotLerp(w.headY[i], h.BodyYaw(), 10)
			continue
		}
		hp := w.headPos(h, i+1)
		tp := e.Position()
		xd, yd, zd := tp[0]-hp[0], tp[1]+eyeHeight(e)-hp[1], tp[2]-hp[2]
		sd := math.Sqrt(xd*xd + zd*zd)
		w.headX[i] = rotLerp(w.headX[i], -(math.Atan2(yd, sd) * 180 / math.Pi), 40)
		w.headY[i] = rotLerp(w.headY[i], math.Atan2(zd, xd)*180/math.Pi-90, 10)
	}
}

// eyeHeight is an entity's eye height (0.85 of its box height for mobs,
// 1.62 for players).
func eyeHeight(e world.Entity) float64 {
	if _, player := e.(interface{ GameMode() world.GameMode }); player {
		return 1.62
	}
	return e.H().Type().BBox(e).Height() * 0.85
}

// headPos is getHeadX/Y/Z: the middle head 3 up, the side heads 1.3 to
// the sides of the body, 2.2 up.
func (w *Wither) headPos(h WitherHost, i int) mgl64.Vec3 {
	pos := h.Position()
	if i <= 0 {
		return pos.Add(mgl64.Vec3{0, 3, 0})
	}
	a := (h.BodyYaw() + 180*float64(i-1)) * math.Pi / 180
	return pos.Add(mgl64.Vec3{math.Cos(a) * 1.3, 2.2, math.Sin(a) * 1.3})
}

// CustomServerAiStep is WitherBoss.customServerAiStep. It returns false
// while the wither is invulnerable (spawning): then the base's
// customServerAiStep is skipped, as Java skips super.customServerAiStep.
func (w *Wither) CustomServerAiStep(h WitherHost) bool {
	tx := h.Tx()
	if w.invulnerable > 0 {
		n := w.invulnerable - 1
		w.updateBar(h, 1-float64(n)/witherInvulnerableTicks)
		if n <= 0 {
			// level.explode(this, x, eyeY, z, 7, false, MOB).
			eye := h.Position().Add(mgl64.Vec3{0, 3.5 * 0.85, 0})
			explodeAt(tx, h, eye, 7, false)
			for p := range tx.Players() {
				tx.PlaySound(p.Position(), entitySound{event: "spawn", id: WitherID})
			}
		}
		w.invulnerable = n
		if h.TickCount()%10 == 0 {
			heal(h, 10)
		}
		return false
	}
	r := h.Rand()
	diff := difficultyID(tx)
	for i := 1; i < 3; i++ {
		if h.TickCount() < w.nextHeadUpdate[i-1] {
			continue
		}
		w.nextHeadUpdate[i-1] = h.TickCount() + 10 + r.IntN(10)
		if diff >= 2 {
			w.idleHeadUpdates[i-1]++
			if w.idleHeadUpdates[i-1]-1 > 15 {
				pos := h.Position()
				x := pos[0] - 10 + r.Float64()*20
				y := pos[1] - 5 + r.Float64()*10
				z := pos[2] - 10 + r.Float64()*20
				w.shoot(h, i+1, mgl64.Vec3{x, y, z}, true)
				w.idleHeadUpdates[i-1] = 0
			}
		}
		if t := w.targets[i]; t != nil {
			e, ok := t.Entity(tx)
			if ok && w.CanAttack(h, e) && e.Position().Sub(h.Position()).LenSqr() <= 900 &&
				lineOfSight(tx, h.Position().Add(mgl64.Vec3{0, 3.5 * 0.85, 0}), entity.EyePosition(e)) {
				w.ShootAt(h, i+1, e)
				w.nextHeadUpdate[i-1] = h.TickCount() + 40 + r.IntN(20)
				w.idleHeadUpdates[i-1] = 0
			} else {
				w.targets[i] = nil
			}
			continue
		}
		var cands []world.Entity
		box := h.H().Type().BBox(h).Translate(h.Position()).GrowVec3(mgl64.Vec3{20, 8, 20})
		for e := range tx.EntitiesWithin(box.Grow(2)) {
			if e.H() == h.H() || !w.CanAttack(h, e) {
				continue
			}
			if !e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
				continue
			}
			// TARGETING_CONDITIONS: forCombat, range 20, line of sight.
			if e.Position().Sub(h.Position()).LenSqr() > 400 ||
				!lineOfSight(tx, h.Position().Add(mgl64.Vec3{0, 3.5 * 0.85, 0}), entity.EyePosition(e)) {
				continue
			}
			cands = append(cands, e)
		}
		if len(cands) > 0 {
			w.targets[i] = cands[r.IntN(len(cands))].H()
		}
	}
	w.targets[0] = nil
	if t, ok := h.Target(); ok {
		w.targets[0] = t.H()
	}
	if w.destroyBlocksTick > 0 {
		w.destroyBlocksTick--
		if w.destroyBlocksTick == 0 && combat.MobGriefing(tx) {
			w.destroyBlocks(h)
		}
	}
	if h.TickCount()%20 == 0 {
		heal(h, 1)
	}
	w.updateBar(h, h.Health()/h.MaxHealth())
	return true
}

// heal adds health up to the maximum.
func heal(h WitherHost, v float64) {
	if h.Health() > 0 {
		h.SetHealth(math.Min(h.MaxHealth(), h.Health()+v))
	}
}

// updateBar shows the bar to the players that see the wither
// (startSeenByPlayer: within its tracking range, 10 chunks).
func (w *Wither) updateBar(h WitherHost, progress float64) {
	w.bar.update(h.Tx(), h, progress, h.TickCount()%20 == 0, func(p world.Entity) bool {
		return p.Position().Sub(h.Position()).LenSqr() <= 160*160
	})
}

// RemoveBar hides the boss bar (the wither died or was removed).
func (w *Wither) RemoveBar(h world.Entity) { w.bar.hide(h) }

// witherImmune is BlockTags.WITHER_IMMUNE (Bedrock names).
var witherImmune = map[string]bool{
	"barrier": true, "bedrock": true, "end_portal": true, "end_portal_frame": true, "end_gateway": true,
	"command_block": true, "repeating_command_block": true, "chain_command_block": true, "structure_block": true,
	"jigsaw": true, "moving_block": true, "light_block": true, "reinforced_deepslate": true,
}

// CanDestroy is WitherBoss.canDestroy: not air and not wither_immune.
func CanDestroy(b world.Block) bool {
	n := blockID(b)
	return n != "air" && !witherImmune[n]
}

// destroyBlocks breaks (with drops) every block the wither may destroy in
// its box grown by 1 sideways (width/2 + 1 around its block, up to its
// height above it).
func (w *Wither) destroyBlocks(h WitherHost) {
	tx := h.Tx()
	box := h.H().Type().BBox(h)
	width := int(math.Floor(box.Width()/2 + 1))
	height := int(math.Floor(box.Height()))
	bp := cube.PosFromVec3(h.Position())
	destroyed := false
	for x := bp[0] - width; x <= bp[0]+width; x++ {
		for y := bp[1]; y <= bp[1]+height; y++ {
			for z := bp[2] - width; z <= bp[2]+width; z++ {
				pos := cube.Pos{x, y, z}
				if pos.OutOfBounds(tx.Range()) {
					continue
				}
				b := tx.Block(pos)
				if !CanDestroy(b) {
					continue
				}
				breakBlock(tx, pos, b)
				destroyed = true
			}
		}
	}
	if destroyed {
		tx.PlaySound(h.Position(), entitySound{event: "break.block", id: WitherID})
	}
}

// breakBlock is Level.destroyBlock(pos, true): the block goes with its
// drops.
func breakBlock(tx *world.Tx, pos cube.Pos, b world.Block) {
	tx.SetBlock(pos, nil, nil)
	if br, ok := b.(block.Breakable); ok {
		for _, d := range br.BreakInfo().Drops(item.ToolNone{}, nil) {
			it := entity.NewItem(world.EntitySpawnOpts{Position: pos.Vec3Centre()}, d)
			tx.AddEntity(it)
		}
	}
}

// ShootAt is performRangedAttack(head, target): at the target's middle
// (eye height x 0.5 above its feet), a blue skull with a 1 in 1000 chance
// from the middle head.
func (w *Wither) ShootAt(h WitherHost, head int, target world.Entity) {
	p := target.Position()
	dangerous := head == 0 && h.Rand().Float32() < 0.001
	w.shoot(h, head, mgl64.Vec3{p[0], p[1] + eyeHeight(target)*0.5, p[2]}, dangerous)
}

// shoot fires a skull from head at pos (performRangedAttack(head, x, y, z,
// dangerous)).
func (w *Wither) shoot(h WitherHost, head int, pos mgl64.Vec3, dangerous bool) {
	tx := h.Tx()
	tx.PlaySound(h.Position(), entitySound{event: "shoot", id: WitherID})
	from := w.headPos(h, head)
	t := WitherSkullType
	if dangerous {
		t = DangerousWitherSkullType
	}
	SpawnHurting(tx, t, HurtingOpts{Owner: h, Pos: from, Dir: pos.Sub(from)})
}

// CanAttack is LIVING_ENTITY_SELECTOR + canAttack: living, attackable
// (not a creative or spectator player) and no undead (wither_friends).
func (w *Wither) CanAttack(h WitherHost, e world.Entity) bool {
	if _, ok := e.(entity.Living); !ok || !targetable(e) {
		return false
	}
	return !undead(e)
}

// undead is EntityTypeTags.UNDEAD.
func undead(e world.Entity) bool {
	switch e.H().Type().EncodeEntity() {
	case "minecraft:skeleton", "minecraft:stray", "minecraft:wither_skeleton", "minecraft:wither", "minecraft:zombie",
		"minecraft:husk", "minecraft:drowned", "minecraft:zombie_villager", "minecraft:zombie_villager_v2",
		"minecraft:zombie_pigman", "minecraft:zombified_piglin", "minecraft:zoglin", "minecraft:phantom",
		"minecraft:skeleton_horse", "minecraft:zombie_horse", "minecraft:bogged", "minecraft:parched",
		"minecraft:camel_husk", "minecraft:zombie_nautilus":
		return true
	}
	return false
}

// FilterDamage is WitherBoss.hurtServer before super: nothing from a
// wither (or the undead), nothing while spawning, arrows and wind charges
// bounce off a powered wither; a hit arms the block breaking (20 ticks) and
// makes the side heads impatient (+3).
func (w *Wither) FilterDamage(h WitherHost, src world.DamageSource) bool {
	var by, direct world.Entity
	switch s := src.(type) {
	case entity.AttackDamageSource:
		by = s.Attacker
	case entity.ProjectileDamageSource:
		by, direct = s.Owner, s.Projectile
	case FireballDamageSource:
		by, direct = s.Owner, s.Projectile
	case entity.DrowningDamageSource:
		// wither_immune_to: drowning.
		return false
	}
	if by != nil && (by.H().Type().EncodeEntity() == WitherID || undead(by)) {
		return false
	}
	if w.invulnerable > 0 {
		if _, void := src.(entity.VoidDamageSource); !void {
			return false
		}
	}
	if w.Powered(h) && direct != nil {
		switch direct.H().Type().EncodeEntity() {
		case "minecraft:arrow", "minecraft:thrown_trident", "minecraft:wind_charge_projectile":
			return false
		}
	}
	if w.destroyBlocksTick <= 0 {
		w.destroyBlocksTick = 20
	}
	for i := range w.idleHeadUpdates {
		w.idleHeadUpdates[i] += 3
	}
	return true
}

// CanBeAffected is WitherBoss.addEffect/canBeAffected: no effects from
// anything but plugins.
func (w *Wither) CanBeAffected(effect.Effect) bool { return false }

// DeathDrops is dropCustomDeathLoot: a nether star that does not despawn
// (setExtendedLifetime).
func (w *Wither) DeathDrops(tx *world.Tx, pos mgl64.Vec3) {
	star := entity.NewItem(world.EntitySpawnOpts{Position: pos.Add(mgl64.Vec3{0, 0.5, 0})}, item.NewStack(item.NetherStar{}, 1))
	tx.AddEntity(star)
}

// EncodeNBT is addAdditionalSaveData.
func (w *Wither) EncodeNBT(m map[string]any) { m["Invul"] = int32(w.invulnerable) }

// DecodeNBT is readAdditionalSaveData.
func (w *Wither) DecodeNBT(m map[string]any) {
	if v, ok := m["Invul"].(int32); ok {
		w.invulnerable = int(v)
	}
}

// explodeAt is Level.explode(source, pos, power, fire, MOB).
func explodeAt(tx *world.Tx, src world.Entity, pos mgl64.Vec3, power float64, fire bool) {
	es := world.EntityExplosionSource{Entity: src, ExplosionSize: power, OffsetY: pos[1] - src.Position()[1]}
	if !combat.MobGriefing(tx) {
		combat.ExplodeEntitiesOnly(tx, src, power)
		return
	}
	block.ExplosionConfig{SpawnFire: fire, Source: src}.Explode(tx, es)
}

// rotLerp is WitherBoss.rotlerp: from a towards b by at most max degrees.
func rotLerp(a, b, max float64) float64 {
	d := wrapDeg(b - a)
	return a + clamp(d, -max, max)
}
