package javasession

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// Java SpellcasterIllager.IllagerSpell ids.
const (
	javaSpellNone      = 0
	javaSpellSummonVex = 1
	javaSpellFangs     = 2
	javaSpellWololo    = 3
)

// javaSpells maps the Bedrock spell colours of the evoker's spells
// (particle_color in its entity file, actor data DataSpellCastingColor) to
// the Java spells of the same colour.
var javaSpells = map[int32]byte{
	int32(-0x4c4c34): javaSpellSummonVex, // #FFB3B3CC
	int32(-0x99b2a7): javaSpellFangs,     // #FF664D59
	int32(-0x4c7fcd): javaSpellWololo,    // #FFB38033
}

// spellData writes Raider.IS_CELEBRATING (16) and SpellcasterIllager.DATA_SPELL_CASTING_ID (17): while the evoker has the casting actor flag,
// the Java spell its spell colour stands for (summon vex for colours Java has no spell of), so the Java client
// raises its arms and shows spell particles of that colour; none otherwise.
func spellData(s *Session, d *mobData, e world.Entity, b string) {
	raiderData(s, d, e, b)
	spell := byte(javaSpellNone)
	if mobFlag(e, protocol.EntityDataFlagCasting) {
		spell = javaSpellSummonVex
		if v, ok := extraData(e, protocol.EntityDataKeyDataSpellCastingColor); ok {
			if c, ok := v.(int32); ok {
				if s, ok := javaSpells[c]; ok {
					spell = s
				}
			}
		}
	}
	d.byteVal(17, spell)
}
