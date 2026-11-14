package javasession

import (
	"bytes"
	"context"
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/java/protocol/wire"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// attrBody is an update_attributes body for the own player with one attribute and no modifiers.
func attrBody(attr string, v float64) []byte {
	w := &wire.Writer{}
	w.VarInt(selfEntityID)
	w.VarInt(1)
	w.VarInt(v777.BuiltinID("minecraft:attribute", "minecraft:"+attr))
	w.Float64(v)
	w.VarInt(0)
	return w.B
}

func TestArmourAttributes(t *testing.T) {
	diamond := [4]item.Stack{
		item.NewStack(item.Helmet{Tier: item.ArmourTierDiamond{}}, 1),
		item.NewStack(item.Chestplate{Tier: item.ArmourTierDiamond{}}, 1),
		item.NewStack(item.Leggings{Tier: item.ArmourTierDiamond{}}, 1),
		item.NewStack(item.Boots{Tier: item.ArmourTierDiamond{}}, 1),
	}
	if v := enchantAttributeValues(item.Stack{}, diamond, 0); v[attrArmor] != 20 || v[attrArmorToughness] != 8 {
		t.Fatalf("diamond set: armor %v toughness %v, want 20 and 8", v[attrArmor], v[attrArmorToughness])
	}
	iron := [4]item.Stack{1: item.NewStack(item.Chestplate{Tier: item.ArmourTierIron{}}, 1)}
	if v := enchantAttributeValues(item.Stack{}, iron, 0); v[attrArmor] != 6 || v[attrArmorToughness] != 0 {
		t.Fatalf("iron chestplate: armor %v toughness %v, want 6 and 0", v[attrArmor], v[attrArmorToughness])
	}
	if v := enchantAttributeValues(item.Stack{}, [4]item.Stack{}, 0); v[attrArmor] != 0 || v[attrArmorToughness] != 0 {
		t.Fatalf("no armour: %v", v)
	}
	for _, n := range []string{"armor", "armor_toughness"} {
		if v777.BuiltinID("minecraft:attribute", "minecraft:"+n) < 0 {
			t.Fatalf("%s not in the attribute registry", n)
		}
	}
}

func TestMaxHealthAttribute(t *testing.T) {
	s := fxTestSession()
	ids := func() (ids []int32, attrs [][]byte) {
		for _, o := range s.take() {
			ids = append(ids, o.id)
			if o.id == v777.ClientboundPlayUpdateAttributes {
				attrs = append(attrs, o.w.B)
			}
		}
		return
	}
	// The first health carries the maximum, before the health (the client clamps to it).
	s.SendHealth(32, 32, 0)
	got, attrs := ids()
	if len(got) < 2 || got[0] != v777.ClientboundPlayUpdateAttributes || got[1] != v777.ClientboundPlaySetHealth {
		t.Fatalf("first health: packets %x, want update_attributes then set_health", got)
	}
	if !bytes.Equal(attrs[0], attrBody("max_health", 32)) {
		t.Fatalf("max_health body %x, want %x", attrs[0], attrBody("max_health", 32))
	}
	// The same maximum again: the health alone.
	s.SendHealth(20, 32, 0)
	if _, attrs = ids(); len(attrs) != 0 {
		t.Fatalf("unchanged maximum sent again")
	}
	// A new maximum (health boost).
	s.SendHealth(20, 40, 0)
	if _, attrs = ids(); len(attrs) != 1 || !bytes.Equal(attrs[0], attrBody("max_health", 40)) {
		t.Fatalf("new maximum: %x", attrs)
	}
	// A new player entity (respawn, world change) gets it again, and the absorption.
	s.newPlayerEntity()
	s.SendHealth(40, 40, 0)
	got, attrs = ids()
	if len(attrs) != 1 || !bytes.Equal(attrs[0], attrBody("max_health", 40)) {
		t.Fatalf("after a new player entity: %x", attrs)
	}
	if got[len(got)-1] != v777.ClientboundPlaySetEntityData {
		t.Fatalf("after a new player entity: no absorption: %x", got)
	}
	if javaMaxHealth(5000) != 1024 || javaMaxHealth(0) != 1 {
		t.Fatalf("javaMaxHealth out of the attribute range")
	}
}

// TestArrowHitPlayerDing shoots an arrow of a Java player into another player: the shooter gets
// game event 6 (the arrow hit "ding"), once.
func TestArrowHitPlayerDing(t *testing.T) {
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
	defer w.Close()
	s := mobTestSession()
	_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		pos := mgl64.Vec3{0.5, 10, 0.5}
		shooter := tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(player.Type, player.Config{
			Name: "Archer", UUID: uuid.New(), GameMode: world.GameModeSurvival, Health: 20, MaxHealth: 20, Position: pos, Session: s,
		})).(*player.Player)
		s.ent = shooter.H()
		vpos := mgl64.Vec3{0.5, 10, 6.5}
		victim := tx.AddEntity(world.EntitySpawnOpts{Position: vpos}.New(player.Type, player.Config{
			Name: "Target", UUID: uuid.New(), GameMode: world.GameModeSurvival, Health: 20, MaxHealth: 20, Position: vpos,
		})).(*player.Player)
		s.take()
		defer tx.RemoveEntity(shooter) // a session player left in the world is closed with it

		arrow := tx.AddEntity(entity.NewArrow(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 11, 2}, Velocity: mgl64.Vec3{0, 0, 2}}, shooter)).(*entity.Ent)
		dings := 0
		for i := int64(0); i < 10; i++ {
			arrow.Tick(tx, i+1)
			for _, o := range s.take() {
				if o.id == v777.ClientboundPlayGameEvent && len(o.w.B) == 5 && o.w.B[0] == gameEventArrowHitPlayer {
					dings++
				}
			}
		}
		if victim.Health() >= 20 {
			t.Fatalf("the arrow did not hit the target (health %v)", victim.Health())
		}
		if dings != 1 {
			t.Fatalf("shooter got %d arrow hit game events, want 1", dings)
		}
		return struct{}{}, nil
	})
}
