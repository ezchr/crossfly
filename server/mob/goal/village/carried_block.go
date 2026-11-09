//go:build crossfly_full

package village

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	ccore "github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// The block an enderman carries (take_block / place_block; BDS EnderMan's
// carried block). Clients see it through actor data 23
// (EnderMan::setCarryingBlock; javasession turns it into Java's
// EnderMan.DATA_CARRY_STATE). It is not a held item, so no equipment drop
// chance applies: the death hook below always drops it. The minecraft:teleport
// component saves it (package goal/misc/hostile), as BDS EnderMan saves
// carriedBlock.

func init() {
	mob.RegisterDeathHook(mob.DeathHook{Name: "village/hold_block", Die: dropCarried})
}

type carriedKey struct{}

// CarriedBlock holds the block a mob carries; a component that saves it
// keeps the pointer (CarriedHolder).
type CarriedBlock struct {
	// Block is the carried block, nil when the mob carries nothing.
	Block world.Block
	// migrated is set once migrateCarried looked at the held items.
	migrated bool
}

// CarriedHolder returns m's carried block holder, creating it.
func CarriedHolder(m *mob.Mob) *CarriedBlock {
	if v, ok := m.Data(carriedKey{}); ok {
		return v.(*CarriedBlock)
	}
	c := &CarriedBlock{}
	m.SetData(carriedKey{}, c)
	return c
}

// Carried returns the block m carries.
func Carried(m *mob.Mob) (world.Block, bool) {
	v, ok := m.Data(carriedKey{})
	if !ok {
		return nil, false
	}
	b := v.(*CarriedBlock).Block
	return b, b != nil
}

// SetCarried is EnderMan::setCarryingBlock: m carries b (nil or air:
// nothing), shown with actor data 23 (the block's runtime id, air when
// empty).
func SetCarried(m *mob.Mob, b world.Block) {
	if _, air := b.(block.Air); air {
		b = nil
	}
	CarriedHolder(m).Block = b
	shown := b
	if shown == nil {
		shown = block.Air{}
	}
	m.SetActorData(protocol.EntityDataKeyCarryBlockRuntimeID, int32(world.BlockRuntimeID(shown)))
}

// migrateCarried moves a block an enderman saved before carried blocks were
// saved (it then held the block's item in its main hand) back into its
// arms, once per mob.
func migrateCarried(m *mob.Mob) {
	c := CarriedHolder(m)
	if c.migrated {
		return
	}
	c.migrated = true
	main, off := m.HeldItems()
	if c.Block != nil || main.Empty() {
		return
	}
	if b, ok := main.Item().(world.Block); ok {
		m.SetHeldItems(item.Stack{}, off)
		SetCarried(m, b)
	}
}

// dropCarried is HoldBlockSystem::_holdBlockSystemOnActorDie: a mob that
// dies carrying a block drops it as an item. The actor data stays, so the
// block shows during the death animation, as in BDS.
func dropCarried(m *mob.Mob, _ world.DamageSource) {
	b, ok := Carried(m)
	if !ok {
		return
	}
	CarriedHolder(m).Block = nil
	if it, ok := b.(world.Item); ok {
		ccore.DropItems(m, []item.Stack{item.NewStack(it, 1)}, 0)
	}
}
