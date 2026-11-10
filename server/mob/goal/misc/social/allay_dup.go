//go:build crossfly_full

package social

import (
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
)

// The allay's hard-coded parts (BDS Allay): a dancing allay given an amethyst shard duplicates
// itself, at most once every 6000 ticks (the cooldown is saved), and an allay heals 1 health
// every 10 ticks (Allay::aiStep).

func init() {
	core.SetDanceGiver(allayDancer{})
	mob.RegisterNBTHook(mob.NBTHook{Name: "social/allay", Encode: encodeAllay, Decode: decodeAllay})
	mob.RegisterSystem(mob.System{Name: "social/allay", Stage: mob.StageSensors, Tick: allayTick})
}

const allayID = "minecraft:allay"

// duplicationCooldown is the cooldown of Allay::setNewDuplicationCooldown, in level ticks.
const duplicationCooldown = 6000

// allayCooldownTag is the NBT key of the cooldown (Allay::addAdditionalSaveData).
const allayCooldownTag = "AllayDuplicationCooldown"

// allayKey keys an allay's *allayState (mob.Data).
type allayKey struct{}

type allayState struct {
	// nextHeal is the allay tick after which it heals again (not saved, as in BDS).
	nextHeal int64
	// cooldown is the level tick (world.Tx.CurrentTick) until which the allay cannot duplicate.
	cooldown int64
}

func allayStateOf(m *mob.Mob) *allayState {
	if v, ok := m.Data(allayKey{}); ok {
		return v.(*allayState)
	}
	st := &allayState{}
	m.SetData(allayKey{}, st)
	return st
}

// allayDancer is the core.DanceGiver of the minecraft:interact give_item of allays.
type allayDancer struct{}

// Dancing ...
func (allayDancer) Dancing(m *mob.Mob) bool { return Dancing(m) }

// DuplicateGive is the duplication of the interaction lambda (InteractComponent::_runInteraction):
// a dancing allay given an amethyst shard without a duplication cooldown duplicates.
func (allayDancer) DuplicateGive(m *mob.Mob, _ item.User, held item.Stack) bool {
	if _, ok := held.Item().(item.AmethystShard); !ok || m.Identifier() != allayID || hasDuplicationCooldown(m) {
		return false
	}
	duplicate(m)
	return true
}

// hasDuplicationCooldown is Allay::hasDuplicationCooldown.
func hasDuplicationCooldown(m *mob.Mob) bool {
	return m.Tx().CurrentTick() < allayStateOf(m).cooldown
}

// setNewDuplicationCooldown is Allay::setNewDuplicationCooldown.
func setNewDuplicationCooldown(m *mob.Mob) {
	allayStateOf(m).cooldown = m.Tx().CurrentTick() + duplicationCooldown
}

// duplicate spawns the copy: love hearts (actor event 21), the chime.amethyst_block sound at the
// allay, a new allay at its position (Spawner::spawnMob, which fires entity_spawned), and both
// get the cooldown.
func duplicate(m *mob.Mob) {
	kit.Show(m, core.LoveHeartsAction{})
	m.Tx().PlaySound(m.EyePosition(), core.MobSound{Event: "chime.amethyst_block", Identifier: allayID})
	setNewDuplicationCooldown(m)
	if c, err := mob.Spawn(m.Tx(), allayID, m.Position(), mob.SpawnOptions{}); err == nil {
		setNewDuplicationCooldown(c)
	}
}

// allayTick is the heal of Allay::aiStep: 1 health once the tick passed the next heal tick, which
// is then 10 ticks later.
func allayTick(m *mob.Mob) {
	if m.Identifier() != allayID {
		return
	}
	st := allayStateOf(m)
	if t := m.Ticks(); t > st.nextHeal {
		if m.Health() < m.MaxHealth() {
			m.Heal(1, effect.RegenerationHealingSource{})
		}
		st.nextHeal = t + 10
	}
}

// encodeAllay saves the duplication cooldown of an allay.
func encodeAllay(m *mob.Mob) map[string]any {
	if m.Identifier() != allayID {
		return nil
	}
	var cd int64
	if v, ok := m.Data(allayKey{}); ok {
		cd = v.(*allayState).cooldown
	}
	return map[string]any{allayCooldownTag: cd}
}

// decodeAllay loads it (Allay::readAdditionalSaveData).
func decodeAllay(m *mob.Mob, d map[string]any) {
	if cd, ok := d[allayCooldownTag].(int64); ok && m.Identifier() == allayID {
		allayStateOf(m).cooldown = cd
	}
}
