package javasession

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// dataTypeOptionalBlockState is EntityDataSerializers.OPTIONAL_BLOCK_STATE: a block state id, 0 for none.
const dataTypeOptionalBlockState = 15

// endermanData is EnderMan.DATA_CARRY_STATE (16), the carried block from the Bedrock carried block
// actor data (23, a runtime id; server/mob goal/village.SetCarried), and DATA_CREEPY (17): angry,
// screaming with the jaw open.
func endermanData(_ *Session, d *mobData, e world.Entity, _ string) {
	d.entry(16, dataTypeOptionalBlockState)
	d.w.VarInt(endermanCarryState(e))
	d.boolean(17, mobBool(e, mobAngry.Angry))
}

// endermanCarryState is the Java state of the block e carries, 0 for none.
func endermanCarryState(e world.Entity) int32 {
	v, ok := extraData(e, protocol.EntityDataKeyCarryBlockRuntimeID)
	if !ok {
		return 0
	}
	rid, ok := v.(int32)
	if !ok || rid < 0 {
		return 0
	}
	b, ok := world.BlockByRuntimeID(uint32(rid))
	if !ok {
		return 0
	}
	if _, air := b.(block.Air); air {
		return 0
	}
	st, _ := javaState(b)
	return st
}
