package javasession

import (
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	jitem "github.com/df-mc/dragonfly/server/java/protocol/item"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// javaEntityName maps Bedrock entity ids whose Java name differs. Ids not listed use the same name.
var javaEntityName = map[string]string{
	"minecraft:xp_orb":                 "minecraft:experience_orb",
	"minecraft:xp_bottle":              "minecraft:experience_bottle",
	"minecraft:ender_crystal":          "minecraft:end_crystal",
	"minecraft:fireworks_rocket":       "minecraft:firework_rocket",
	"minecraft:thrown_trident":         "minecraft:trident",
	"minecraft:wind_charge_projectile": "minecraft:wind_charge",
	"minecraft:fishing_hook":           "minecraft:fishing_bobber",
	"minecraft:villager_v2":            "minecraft:villager",
	"minecraft:zombie_villager_v2":     "minecraft:zombie_villager",
	"minecraft:evocation_illager":      "minecraft:evoker",
	"minecraft:evocation_fang":         "minecraft:evoker_fangs",
	"minecraft:vindicator":             "minecraft:vindicator",
	"minecraft:zombie_pigman":          "minecraft:zombified_piglin",
	"minecraft:snow_golem":             "minecraft:snow_golem",
	"minecraft:leash_knot":             "minecraft:leash_knot",
	"minecraft:tripod_camera":          "",
	"minecraft:npc":                    "",
	"minecraft:agent":                  "",
	"minecraft:test_moving_ent":        "",
	"minecraft:cushion":                "minecraft:cushion", // 26.3; older versions have none
	"dragonfly:text":                   "minecraft:text_display",

	// Bedrock ids Java spells differently. minecraft:boat and minecraft:chest_boat are one id for
	// every wood: javaTypeName picks the Java type by their Variant.
	"minecraft:tropicalfish":                  "minecraft:tropical_fish",
	"minecraft:eye_of_ender_signal":           "minecraft:eye_of_ender",
	"minecraft:breeze_wind_charge_projectile": "minecraft:breeze_wind_charge",
	"minecraft:wither_skull_dangerous":        "minecraft:wither_skull", // with DATA_DANGEROUS (mobdata.go)
	"minecraft:elder_guardian_ghost":          "",                       // Bedrock's curse animation actor
	"minecraft:balloon":                       "",                       // Education Edition
	"minecraft:ice_bomb":                      "",                       // Education Edition
}

// RegisterEntityAlias shows entities of a custom Bedrock type (a server's own entity, such as
// "example:pet") to Java players as a Java entity type ("minecraft:rabbit"). An empty java hides
// them. Call it before players join.
func RegisterEntityAlias(bedrock, java string) {
	javaEntityName[bedrock] = java
}

// networkEncoded is implemented by entities with a network id other than their save id.
type networkEncoded interface {
	NetworkEncodeEntity() string
}

// bedrockName is the Bedrock entity id of e.
func bedrockName(e world.Entity) string {
	if n, ok := e.(networkEncoded); ok {
		return n.NetworkEncodeEntity()
	}
	return e.H().Type().EncodeEntity()
}

// javaTypeName is the Java entity type name e is shown as, or ok == false for entities Java has no
// equivalent of.
func javaTypeName(e world.Entity) (string, bool) {
	name := bedrockName(e)
	switch name {
	case "minecraft:boat":
		return boatType(e, bedrockBoatToJava), true
	case "minecraft:chest_boat":
		return boatType(e, bedrockChestBoatToJava), true
	}
	if j, listed := javaEntityName[name]; listed {
		return j, j != ""
	}
	return name, true
}

// boatType is the Java boat type of the wood a Bedrock boat's Variant names (oak if unknown).
func boatType(e world.Entity, types []string) string {
	if t := nth(types, mobInt(e, mobVariant.Variant, 0)); t != "" {
		return t
	}
	return types[0]
}

// isBoat reports whether e is a Bedrock boat, whose yaw is 90 degrees off Java's.
func isBoat(e world.Entity) bool {
	n := bedrockName(e)
	return n == "minecraft:boat" || n == "minecraft:chest_boat"
}

// javaEntity is the Java entity type of e and the add_entity data field, or ok == false for
// entities Java has no equivalent of (they stay invisible to Java players).
func javaEntity(e world.Entity) (typ, data int32, ok bool) {
	name, ok := javaTypeName(e)
	if !ok {
		return 0, 0, false
	}
	typ = v777.BuiltinID("minecraft:entity_type", name)
	if typ < 0 {
		return 0, 0, false
	}
	if name == "minecraft:painting" {
		// HangingEntity.setDirection: the data is the 3D id of a horizontal direction; anything
		// else (0, down) makes the client throw and drop the connection.
		data = 3 // south
		if f, ok := mobAs[paintingFacing](e); ok {
			data = int32(f.Facing()) + 2 // north 2, south 3, west 4, east 5
		}
	}
	if ent, ok := e.(*entity.Ent); ok {
		if fb, ok := ent.Behaviour().(*entity.FallingBlockBehaviour); ok {
			// falling_block carries its block state in the data field.
			rid := world.BlockRuntimeID(fb.Block())
			if bi := blocks(); int(rid) < len(bi.java) {
				data = int32(bi.java[rid])
			}
		}
	}
	return typ, data, true
}

// skippedTypes are the entity types already logged as having no Java equivalent.
var skippedTypes sync.Map

// velocity of e, if it has one.
func velocity(e world.Entity) mgl64.Vec3 {
	if v, ok := e.(interface{ Velocity() mgl64.Vec3 }); ok {
		return v.Velocity()
	}
	return mgl64.Vec3{}
}

// viewOtherEntity spawns a non-player entity.
func (s *Session) viewOtherEntity(e world.Entity) {
	typ, data, ok := javaEntity(e)
	if ok {
		// The client's own ids: entity types and the falling block's state.
		if typ = s.ver.Builtin("minecraft:entity_type", typ); typ < 0 {
			ok = false
		} else if typ == s.ver.BuiltinID("minecraft:entity_type", "minecraft:falling_block") {
			data = s.ver.BlockState(data)
		}
	}
	if !ok {
		if _, seen := skippedTypes.LoadOrStore(e.H().Type().EncodeEntity(), true); !seen {
			s.log.Info("no Java entity for this type; Java players do not see it", "type", e.H().Type().EncodeEntity())
		}
		return
	}
	pos, rot, vel := e.Position(), e.Rotation(), velocity(e)
	switch typ {
	case s.ver.BuiltinID("minecraft:entity_type", "minecraft:fishing_bobber"):
		// FishingHook.recreateFromPacket: the data is the id of the player that cast it, else the
		// client discards the hook.
		owner, ok := s.ownerID(e)
		if !ok {
			return
		}
		data = owner
	case s.ver.BuiltinID("minecraft:entity_type", "minecraft:painting"):
		// A painting is placed at the block it hangs in (BlockAttachedEntity.setPos).
		b := cube.PosFromVec3(pos)
		if hb, ok := mobAs[paintingBlock](e); ok {
			b = hb.HangingBlock()
		}
		pos = mgl64.Vec3{float64(b[0]), float64(b[1]), float64(b[2])}
	}
	id := s.addEntityID(e)
	body, head := entityYaws(e, rot)
	u := e.H().UUID()
	w := s.packet()
	w.VarInt(id)
	w.UUID(u)
	w.VarInt(typ)
	w.Float64(pos[0])
	w.Float64(pos[1])
	w.Float64(pos[2])
	w.LpVec3(vel[0], vel[1], vel[2])
	w.Angle(float32(rot.Pitch()))
	w.Angle(float32(body))
	w.Angle(float32(head))
	w.VarInt(data)
	s.queue(v777.ClientboundPlayAddEntity, w)
	s.setTrackHead(id, pos, cube.Rotation{body, rot.Pitch()}, head)
	s.viewEntityMeta(e, id)
	if textModelFor(e) != nil || isTextEntity(e) {
		s.viewCustomName(e, id)
	} else {
		// Its state as it is now: flags (on fire, invisible), name, mob data (baby, variants...),
		// saddle. Without it a mob shows its defaults until its state next changes.
		s.ViewEntityState(e)
	}
	s.viewModel(e, id, pos, rot)
	s.noteRiding(e)
}

// viewEntityMeta sends entity data a freshly spawned entity needs: an item entity's item.
func (s *Session) viewEntityMeta(e world.Entity, id int32) {
	if m := textModelFor(e); m != nil {
		s.viewTextModel(e, id, m)
		return
	}
	if isTextEntity(e) {
		s.viewTextDisplay(e, id, true)
		return
	}
	if c, ok := e.(*entity.Cushion); ok {
		s.viewCushionColour(c, id)
		return
	}
	ent, ok := e.(*entity.Ent)
	if !ok {
		return
	}
	ib, ok := ent.Behaviour().(*entity.ItemBehaviour)
	if !ok {
		return
	}
	var js jitem.Stack
	javaStack(ib.Item(), &js)
	w := s.packet()
	w.VarInt(id)
	w.Byte(8)                        // ItemEntity.DATA_ITEM
	w.VarInt(7)                      // ITEM_STACK serializer
	js.EncodeFor(w, s.items().proto) // item and component ids of the client version
	w.Byte(0xff)
	s.queue(v777.ClientboundPlaySetEntityData, w)
}
