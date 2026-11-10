package player

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/gameevent"
	"github.com/go-gl/mathgl/mgl64"
)

// The game events a player makes (package gameevent), heard by wardens and sculk blocks. None of this
// runs in a world without vibration listeners.

// postGameEvent posts ev at pos, made by the player. Spectators make none.
func (p *Player) postGameEvent(ev gameevent.Event, pos mgl64.Vec3) {
	if !gameevent.Active(p.tx) || p.GameMode() == world.GameModeSpectator {
		return
	}
	gameevent.Post(p.tx, ev, pos, p)
}

// trackGameEventMovement posts the steps, swims and landings of the player
// (GameEventMovementTrackingSystem::_trackMovement, BlockLegacy::onFallOnBase).
func (p *Player) trackGameEventMovement() {
	if !gameevent.Active(p.tx) || p.GameMode() == world.GameModeSpectator {
		return
	}
	if p.gameEventMove == nil {
		p.gameEventMove = &gameevent.Movement{}
	}
	pos := p.Position()
	feet := cube.PosFromVec3(pos)
	_, inLiquid := p.tx.Liquid(feet)
	ev, ok := p.gameEventMove.Track(gameevent.MoveState{
		Pos: pos, OnGround: p.OnGround(), InLiquid: inLiquid, Climbing: climbable(p.tx.Block(feet)),
		EmitMove: true, EmitSwim: true,
	})
	if ok {
		gameevent.Post(p.tx, ev, pos, p)
	}
}

// climbable reports whether b is one of the blocks _trackMovement counts as moving on: ladders, vines,
// scaffolding, powder snow and cobwebs.
func climbable(b world.Block) bool {
	name, _ := b.EncodeBlock()
	switch name {
	case "minecraft:ladder", "minecraft:vine", "minecraft:scaffolding", "minecraft:powder_snow", "minecraft:web",
		"minecraft:twisting_vines", "minecraft:weeping_vines", "minecraft:cave_vines",
		"minecraft:cave_vines_body_with_berries", "minecraft:cave_vines_head_with_berries":
		return true
	}
	return false
}

// postDamageEvent posts entity_damage for the player being hurt (Mob::_hurt), with the attacker (a
// projectile's shooter) as its owner.
func (p *Player) postDamageEvent(src world.DamageSource) {
	if !gameevent.Active(p.tx) {
		return
	}
	var owner world.Entity
	switch s := src.(type) {
	case entity.AttackDamageSource:
		owner = s.Attacker
	case entity.ProjectileDamageSource:
		owner = s.Owner
	case interface{ DamagingEntity() world.Entity }:
		owner = s.DamagingEntity()
	}
	gameevent.PostContext(p.tx, gameevent.EntityDamage, p.Position(), gameevent.Context{Source: p, Owner: owner})
}

// postConsumeEvent posts eat, or drink for potions, honey and milk (Player::eat, PotionItem, BucketItem).
func (p *Player) postConsumeEvent(s item.Stack) {
	ev := gameevent.Eat
	switch s.Item().(type) {
	case item.Potion, item.HoneyBottle, item.Bucket:
		ev = gameevent.Drink
	}
	p.postGameEvent(ev, p.Position())
}

// postEquipEvent posts equip or unequip for a change of armour (Actor equipment changes).
func (p *Player) postEquipEvent(removed bool) {
	if p.tx == nil {
		return
	}
	ev := gameevent.Equip
	if removed {
		ev = gameevent.Unequip
	}
	p.postGameEvent(ev, p.Position())
}

// wardenThreat is the player's WardenSpawnTrackerComponent: the warning level sculk shriekers raise, the
// cooldown before it can rise again and the ticks until it drops by one.
type wardenThreat struct {
	level, cooldown, decline int
}

// WardenThreat returns the player's warden warning level (0-4), the ticks before a sculk shrieker can
// raise it again and the ticks until it drops by one (WardenThreatLevel,
// WardenThreatLevelIncreaseCooldown and WardenThreatDecreaseTimer in BDS). It is saved with the player.
func (p *Player) WardenThreat() (level, cooldown, decreaseTimer int) {
	return p.wardenThreat.level, p.wardenThreat.cooldown, p.wardenThreat.decline
}

// SetWardenThreat sets the player's warden warning level, cooldown and decrease timer (see WardenThreat).
func (p *Player) SetWardenThreat(level, cooldown, decreaseTimer int) {
	p.wardenThreat = wardenThreat{level: level, cooldown: cooldown, decline: decreaseTimer}
}
