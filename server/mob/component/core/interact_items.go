//go:build crossfly_full

package core

import (
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// The minecraft:interact keys of BDS InteractComponent::_runInteraction (the
// interaction lambda) that interact.go leaves to this file: add_items,
// give_item/take_item, particle_on_start, vibration, and interact_text
// (InteractComponent::getInteraction).

// particleOnStart is particle_on_start (Interaction::setParticleType and
// the particle offsets).
type particleOnStart struct {
	Type    string  `json:"particle_type"`
	YOffset float64 `json:"particle_y_offset"`
	Towards bool    `json:"particle_offset_towards_interactor"`
}

// interactParticles are the particle_on_start particles by their
// ParticleTypeMap name, the names the entity files use. Java has no food
// particle without an item, so Java clients see none for it.
var interactParticles = map[string]world.Particle{
	"smoke":        particle.Legacy{Bedrock: packet.ParticleTypeSmoke, Java: "smoke"},
	"largeexplode": particle.Legacy{Bedrock: packet.ParticleTypeLargeExplode, Java: "explosion"},
	"food":         particle.Legacy{Bedrock: packet.ParticleTypeFood},
}

// emit shows the particle as the interaction lambda does: at the mob's body
// (Actor::getAttachPos Body) raised by particle_y_offset, moved one block
// towards the player's eyes with particle_offset_towards_interactor.
func (p *particleOnStart) emit(m *mob.Mob, user item.User) {
	pa, ok := interactParticles[p.Type]
	if !ok {
		return
	}
	pos := m.Position().Add(mgl64.Vec3{0, m.BBox().Height()*0.5 + p.YOffset})
	if p.Towards {
		if d := entity.EyePosition(user).Sub(pos); d.Len() >= 0.0001 {
			pos = pos.Add(d.Normalize())
		}
	}
	m.Tx().AddParticle(pos, pa)
}

// DanceGiver is the dancing allay, kept by another package: the interaction
// lambda lets a dancing allay given an amethyst shard duplicate itself in
// place of taking the item, and _runInteraction runs give_item for a dancing
// mob even when it holds an item already.
type DanceGiver interface {
	// Dancing reports whether m dances.
	Dancing(m *mob.Mob) bool
	// DuplicateGive is called for a dancing m given held; it reports
	// whether m duplicated (one of held is then used up).
	DuplicateGive(m *mob.Mob, user item.User, held item.Stack) bool
}

var danceGiver DanceGiver

// SetDanceGiver installs the dancing allay. Call it from init only.
func SetDanceGiver(d DanceGiver) { danceGiver = d }

func dancing(m *mob.Mob) bool { return danceGiver != nil && danceGiver.Dancing(m) }

// mayRun is the give_item check of _runInteraction: an interaction that
// gives the mob the held item is skipped while both the mob and the player
// hold one, unless the mob dances.
func (in *interaction) mayRun(m *mob.Mob, held item.Stack) bool {
	if !in.GiveItem || held.Empty() {
		return true
	}
	main, _ := m.HeldItems()
	return main.Empty() || dancing(m)
}

// runGiveTake is give_item and take_item. give_item: the mob takes one of
// the item the player held (before) into its main hand, marked as picked
// up, and an allay likes the player (Actor::setOwner); a dancing mob may
// duplicate instead. take_item, when nothing was given and the hand is
// empty: the player gets the mob's main hand item in the hand and an allay
// forgets its owner. held is what the player's hand holds after.
func (in *interaction) runGiveTake(m *mob.Mob, user item.User, held, before item.Stack, creative bool) item.Stack {
	allay := m.Identifier() == "minecraft:allay"
	given := false
	if in.GiveItem && !held.Empty() && !before.Empty() {
		main, off := m.HeldItems()
		switch {
		case dancing(m) && danceGiver.DuplicateGive(m, user, before):
			given = true
		case main.Empty():
			given = true
			m.SetHeldItems(loot.MarkPickedUp(before.Grow(1-before.Count())), off)
			PlayMobSound(m, "item_given")
			if allay {
				m.SetOwner(user)
			}
		}
		if given && !creative {
			held = held.Grow(-1)
		}
	}
	if in.TakeItem && !given && held.Empty() {
		if main, off := m.HeldItems(); !main.Empty() {
			// BDS adds it to the inventory (Player::add); the hand is
			// empty, and what Interact returns becomes the hand.
			m.SetHeldItems(item.Stack{}, off)
			held = loot.ClearPickedUp(main)
			PlayMobSound(m, "item_taken")
			if allay {
				m.SetOwner(nil)
			}
		}
	}
	return held
}

// runAddItems is add_items: the loot table, rolled with the mob as this
// entity, goes to the player (PlayerUtils::addItem). The first item goes to
// the hand when the interaction emptied it (one bowl gives the stew in the
// hand). held is what is left in the player's hand.
func (in *interaction) runAddItems(m *mob.Mob, user item.User, held item.Stack) item.Stack {
	if in.AddItems == nil || in.AddItems.Table == "" {
		return held
	}
	t, err := LoadLootTable(in.AddItems.Table)
	if err != nil {
		return held
	}
	for _, s := range t.Roll(&LootContext{Mob: m}) {
		if held.Empty() {
			held = s
			continue
		}
		GiveItem(m, user, s)
	}
	return held
}

// runEffects is particle_on_start and vibration (a game event at the mob,
// GameEventUtils::requestDeferredPosting; "none" emits nothing).
func (in *interaction) runEffects(m *mob.Mob, user item.User) {
	if in.ParticleOnStart != nil {
		in.ParticleOnStart.emit(m, user)
	}
	if in.Vibration != "" && in.Vibration != "none" {
		m.EmitVibration(in.Vibration)
	}
}

// InteractText implements mob.InteractTexter: the interact_text of the
// interaction a player holding its item would run (InteractComponent::
// getInteraction sets the button text of the first interaction that
// passes).
func (c *interact) InteractText(m *mob.Mob, user world.Entity) string {
	u, ok := user.(item.User)
	if !ok || m.Ticks() < c.cooldown {
		return ""
	}
	held, _ := u.HeldItems()
	ctx := InteractContext(m, u, held)
	for _, in := range c.list {
		if !in.OnInteract.Passes(ctx) || !in.mayRun(m, held) {
			continue
		}
		if in.CooldownHurt > 0 {
			if _, t, ok := m.LastHurtBy(); ok && m.Ticks()-t < int64(ticksOf(in.CooldownHurt)) {
				return ""
			}
		}
		return in.InteractText
	}
	return ""
}
