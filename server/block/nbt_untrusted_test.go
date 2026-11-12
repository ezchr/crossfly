package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
)

// Block item NBT comes from client packets too: values of the wrong shape or out of range are
// ignored, never a panic, and never kept where they would panic later.

func TestBannerBadPatterns(t *testing.T) {
	b := Banner{}.DecodeNBT(map[string]any{"Patterns": []any{
		"x", int32(1), []any{}, map[string]any{}, map[string]any{"Pattern": "zz"},
		map[string]any{"Pattern": "bo", "Color": int32(1)},
	}}).(Banner)
	if len(b.Patterns) != 1 || b.Patterns[0].Type != BorderBannerPattern() {
		t.Fatalf("patterns %+v", b.Patterns)
	}
	l := BannerPatternLayer{Type: GradientBannerPattern()}.DecodeNBT(map[string]any{"Pattern": "zz"}).(BannerPatternLayer)
	if l.Type != GradientBannerPattern() {
		t.Fatalf("unknown pattern changed the layer: %+v", l)
	}
}

func TestDecoratedPotBadSherds(t *testing.T) {
	p := DecoratedPot{}.DecodeNBT(map[string]any{"sherds": []any{
		"minecraft:stone", int32(1), "x", "minecraft:brick", "minecraft:archer_pottery_sherd",
	}}).(DecoratedPot)
	for i, d := range p.Decorations[:3] {
		if d != nil {
			t.Errorf("sherd %d: %#v", i, d)
		}
	}
	if _, ok := p.Decorations[3].(item.Brick); !ok {
		t.Errorf("sherd 3: %#v", p.Decorations[3])
	}
	_ = p.EncodeNBT()
}

func TestBedColourOutOfRange(t *testing.T) {
	for _, c := range []uint8{16, 200, 255} {
		b := Bed{Colour: item.ColourRed()}.DecodeNBT(map[string]any{"color": c}).(Bed)
		if b.Colour != item.ColourRed() {
			t.Errorf("colour %d: %v", c, b.Colour)
		}
	}
	if b := (Bed{}).DecodeNBT(map[string]any{"color": uint8(15)}).(Bed); b.Colour != item.Colours()[15] {
		t.Errorf("colour 15: %v", b.Colour)
	}
}

func TestBeaconInstantEffectsAndLevel(t *testing.T) {
	instantHealth, _ := effect.ID(effect.InstantHealth)
	instantDamage, _ := effect.ID(effect.InstantDamage)
	b := Beacon{}.DecodeNBT(map[string]any{"Primary": int32(instantHealth), "Secondary": int32(instantDamage), "Levels": int32(1 << 30)}).(Beacon)
	if b.Primary != nil || b.Secondary != nil || b.Level() != 4 {
		t.Fatalf("beacon %+v level %d", b, b.Level())
	}
	if b := (Beacon{}).DecodeNBT(map[string]any{"Levels": int32(-5)}).(Beacon); b.Level() != 0 {
		t.Fatalf("level %d", b.Level())
	}
	speed, _ := effect.ID(effect.Speed)
	if b := (Beacon{}).DecodeNBT(map[string]any{"Primary": int32(speed)}).(Beacon); b.Primary != effect.Speed {
		t.Fatalf("primary %v", b.Primary)
	}
}

func TestSkullTypeOutOfRange(t *testing.T) {
	for _, v := range []uint8{7, 99, 254, 255} {
		s := Skull{Type: CreeperHead()}.DecodeNBT(map[string]any{"SkullType": v}).(Skull)
		if s.Type != CreeperHead() {
			t.Errorf("type %d: %v", v, s.Type)
		}
		_, _ = s.EncodeItem()
		_, _ = s.EncodeBlock()
	}
	if s := (Skull{}).DecodeNBT(map[string]any{"SkullType": uint8(5)}).(Skull); s.Type != DragonHead() {
		t.Errorf("type 5: %v", s.Type)
	}
}

func TestCopperGolemStatuePoseOutOfRange(t *testing.T) {
	for _, v := range []int32{-1, 4, 255, 256, 1 << 30} {
		c := CopperGolemStatue{Pose: SittingPose()}.DecodeNBT(map[string]any{"Pose": v}).(CopperGolemStatue)
		if c.Pose != SittingPose() {
			t.Errorf("pose %d: %v", v, c.Pose)
		}
	}
	if c := (CopperGolemStatue{}).DecodeNBT(map[string]any{"Pose": int32(3)}).(CopperGolemStatue); c.Pose != StarPose() {
		t.Errorf("pose 3: %v", c.Pose)
	}
}

func TestShulkerBoxFacingOutOfRange(t *testing.T) {
	for _, v := range []uint8{6, 200, 255} {
		s := ShulkerBox{Facing: cube.FaceNorth}.DecodeNBT(map[string]any{"facing": v}).(ShulkerBox)
		if s.Facing != cube.FaceNorth {
			t.Errorf("facing %d: %v", v, s.Facing)
		}
	}
	if s := (ShulkerBox{}).DecodeNBT(map[string]any{"facing": uint8(cube.FaceEast)}).(ShulkerBox); s.Facing != cube.FaceEast {
		t.Errorf("facing east: %v", s.Facing)
	}
}
