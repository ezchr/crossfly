package javasession

import (
	"strings"

	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/go-gl/mathgl/mgl64"
)

// levelSound is a mob sound event (server/mob.Sound): a Bedrock level sound
// event name played by an entity type.
type levelSound interface {
	LevelSoundEvent() (event, entityType string, baby bool)
}

// mobSound plays a Bedrock mob sound event as the Java sound vanilla plays
// for the same thing (entity.<type>.<event>). Events the Java client plays
// itself (hurt, death) or has no sound for are dropped.
func (s *Session) mobSound(pos mgl64.Vec3, ls levelSound) {
	event, typ, baby := ls.LevelSoundEvent()
	name, ok := javaMobSoundName(event, typ)
	if !ok {
		return
	}
	id := v777.BuiltinID("minecraft:sound_event", name)
	if id < 0 {
		return
	}
	src := srcNeutral
	if hostileSoundTypes[typ] {
		src = srcHostile
	}
	pitch := (rnd()-rnd())*0.2 + 1 // LivingEntity.getVoicePitch
	if baby {
		pitch += 0.5
	}
	s.soundID(pos, id, src, 1, pitch)
}

// hostileSoundTypes play in the hostile sound category on Java.
var hostileSoundTypes = map[string]bool{
	"minecraft:zombie": true, "minecraft:skeleton": true, "minecraft:creeper": true, "minecraft:spider": true,
	"minecraft:cave_spider": true, "minecraft:husk": true, "minecraft:stray": true, "minecraft:drowned": true,
	"minecraft:witch": true, "minecraft:slime": true, "minecraft:enderman": true, "minecraft:zombie_villager_v2": true,
}

// javaMobSoundName maps a Bedrock level sound event of an entity type to a Java sound event name.
func javaMobSoundName(event, typ string) (string, bool) {
	jt := typ
	if j, listed := javaEntityName[typ]; listed {
		if j == "" {
			return "", false
		}
		jt = j
	}
	short := strings.TrimPrefix(jt, "minecraft:")
	switch event {
	case "hurt", "hurt.baby", "death", "death.baby", "attack.strong", "attack.nodamage", "step", "jump", "fall":
		// The Java client plays these itself (damage event, death entity
		// event 3, walking, landing).
		return "", false
	case "ambient", "ambient.baby", "ambient.tame":
		return "minecraft:entity." + short + ".ambient", true
	case "plop":
		if short == "chicken" {
			return "minecraft:entity.chicken.egg", true
		}
		return "minecraft:entity.item.pickup", true
	case "milk":
		if short == "goat" {
			return "minecraft:entity.goat.milk", true
		}
		return "minecraft:entity.cow.milk", true
	case "shear":
		if short == "mooshroom" {
			return "minecraft:entity.mooshroom.shear", true
		}
		return "minecraft:entity.sheep.shear", true
	case "ignite":
		return "minecraft:item.flintandsteel.use", true
	case "fuse":
		return "minecraft:entity.creeper.primed", true
	case "bow":
		switch short {
		case "skeleton", "stray", "bogged", "wither_skeleton", "parched":
			return "minecraft:entity." + short + ".shoot", true
		}
		return "minecraft:entity.arrow.shoot", true
	case "eat":
		return "minecraft:entity.generic.eat", true
	case "explode":
		return "minecraft:entity.generic.explode", true
	case "saddle":
		if short == "pig" || short == "strider" {
			return "minecraft:entity." + short + ".saddle", true
		}
		return "minecraft:entity.horse.saddle", true
	}
	name := "minecraft:entity." + short + "." + strings.ReplaceAll(event, ".", "_")
	return name, validSoundName(name)
}
