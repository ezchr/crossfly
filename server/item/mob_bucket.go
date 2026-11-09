//go:build crossfly_full

package item

import (
	"maps"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

// MobBucket is a water bucket holding a mob: a bucket of cod, salmon, pufferfish, tropical fish, axolotl or
// tadpole (BDS BucketItem with the fill types Fish, Salmon, TropicalFish, Pufferfish, Axolotl and Tadpole). Used
// on a block it places water and lets the mob out, as it was when it was bucketed.
type MobBucket struct {
	// Mob is the entity identifier of the mob in the bucket, e.g. "minecraft:tropicalfish".
	Mob string
	// data is the little endian NBT of the mob's save data (BDS keeps it as the item's user data), empty for a
	// bucket that holds no particular mob (one from the creative inventory): that one lets a new mob out.
	// It is kept encoded so that the item stays comparable.
	data string
}

// mobBuckets are the mobs that go in buckets and their bucket items (BDS BucketItem::_useOn).
var mobBuckets = map[string]string{
	"minecraft:cod":          "minecraft:cod_bucket",
	"minecraft:salmon":       "minecraft:salmon_bucket",
	"minecraft:pufferfish":   "minecraft:pufferfish_bucket",
	"minecraft:tropicalfish": "minecraft:tropical_fish_bucket",
	"minecraft:axolotl":      "minecraft:axolotl_bucket",
	"minecraft:tadpole":      "minecraft:tadpole_bucket",
}

// NewMobBucket returns the bucket of the mob identifier holding the mob saved in data (nil for none). ok is
// false if the mob does not go in a bucket.
func NewMobBucket(mob string, data map[string]any) (b MobBucket, ok bool) {
	if _, ok := mobBuckets[mob]; !ok {
		return MobBucket{}, false
	}
	b = MobBucket{Mob: mob}
	if len(data) > 0 {
		if enc, err := nbt.MarshalEncoding(data, nbt.LittleEndian); err == nil {
			b.data = string(enc)
		}
	}
	return b, true
}

// MobBuckets returns a bucket of every mob that goes in one, holding no particular mob.
func MobBuckets() []MobBucket {
	return []MobBucket{{Mob: "minecraft:cod"}, {Mob: "minecraft:salmon"}, {Mob: "minecraft:pufferfish"},
		{Mob: "minecraft:tropicalfish"}, {Mob: "minecraft:axolotl"}, {Mob: "minecraft:tadpole"}}
}

// Data returns the save data of the mob in the bucket, or nil if it holds no particular mob. The map is new on
// every call.
func (b MobBucket) Data() map[string]any {
	if b.data == "" {
		return nil
	}
	var m map[string]any
	if err := nbt.UnmarshalEncoding([]byte(b.data), &m, nbt.LittleEndian); err != nil {
		return nil
	}
	return m
}

// BucketMobs lets the mobs of mob buckets out. Package mob/component/core implements it and installs it with
// SetBucketMobs, so this package needs no mob package.
type BucketMobs interface {
	// ReleaseFromBucket spawns the mob of identifier mob in the block at pos, from data if it is not nil (as
	// it was bucketed) or else as a new mob. The mob never despawns. It reports whether a mob came out.
	ReleaseFromBucket(tx *world.Tx, mob string, data map[string]any, pos cube.Pos) bool
}

// bucketMobs is the BucketMobs of the mob buckets, set by SetBucketMobs.
var bucketMobs BucketMobs

// SetBucketMobs installs what lets the mobs of mob buckets out. Call it from an init function.
func SetBucketMobs(b BucketMobs) { bucketMobs = b }

func init() {
	mobItems = append(mobItems, func() {
		for _, b := range MobBuckets() {
			world.RegisterItem(b)
		}
	})
}

// MaxCount always returns 1.
func (MobBucket) MaxCount() int { return 1 }

// UseOnBlock places water where a water bucket would and lets the mob out in it, as BDS BucketItem::_emptyBucket
// with readBucketEntitySaveData: the mob first, then the water, then the bucket.empty.fish sound. The bucket
// turns into an empty one, except in creative mode.
func (b MobBucket) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	it, ok := world.ItemByName("minecraft:water_bucket", 0)
	if !ok {
		return false
	}
	water, ok := it.(Bucket)
	if !ok {
		return false
	}
	liquid, ok := water.Content.Liquid()
	if !ok {
		return false
	}
	liq := liquid.WithDepth(8, false)
	at := pos
	if bl := tx.Block(at); !canDisplace(bl, liq) && !replaceableWith(bl, liq) {
		at = pos.Side(face)
		if bl := tx.Block(at); !canDisplace(bl, liq) && !replaceableWith(bl, liq) {
			return false
		}
	}
	if bucketMobs != nil {
		bucketMobs.ReleaseFromBucket(tx, b.Mob, b.Data(), at)
	}
	tx.SetLiquid(at, liq)
	// BDS plays the sound at the user.
	tx.PlaySound(user.Position(), MobBucketSound{Event: "bucket.empty.fish", Mob: b.Mob})

	ctx.NewItem = NewStack(Bucket{}, 1)
	ctx.NewItemSurvivalOnly = true
	ctx.SubtractFromCount(1)
	return true
}

// MobBucketSound is the sound of a mob going into a bucket ("bucket.fill.fish") or coming out of one
// ("bucket.empty.fish"): the Bedrock level sound event BDS plays for every mob bucket. The Java session plays
// the bucket sound of Mob.
type MobBucketSound struct {
	Event string
	Mob   string
}

// Play ...
func (MobBucketSound) Play(*world.World, mgl64.Vec3) {}

// LevelSoundEvent returns the level sound event, the mob and false (the sound is no baby sound).
func (s MobBucketSound) LevelSoundEvent() (event, entityType string, baby bool) {
	return s.Event, s.Mob, false
}

// EncodeItem ...
func (b MobBucket) EncodeItem() (name string, meta int16) {
	return mobBuckets[b.Mob], 0
}

// mobBucketItemKeys are the keys of an item's tag that the stack itself writes, not the mob in the bucket.
var mobBucketItemKeys = [...]string{"RepairCost", "Damage", "display", "dragonflyData", "ench", "Unbreakable"}

// EncodeNBT returns the save data of the mob in the bucket, with the keys BDS adds for the item's name.
func (b MobBucket) EncodeNBT() map[string]any {
	if m := b.Data(); m != nil {
		return m
	}
	return map[string]any{}
}

// DecodeNBT ...
func (b MobBucket) DecodeNBT(data map[string]any) any {
	m := maps.Clone(data)
	for _, k := range mobBucketItemKeys {
		delete(m, k)
	}
	b.data = ""
	if len(m) > 0 {
		if enc, err := nbt.MarshalEncoding(m, nbt.LittleEndian); err == nil {
			b.data = string(enc)
		}
	}
	return b
}

// Variants returns the variant, mark variant and colours BDS writes in the save data of the mob in the bucket
// (Variant, MarkVariant, Color, Color2), and whether it has them.
func (b MobBucket) Variants() (variant, markVariant, colour, colour2 int32, ok bool) {
	m := b.Data()
	if m == nil {
		return 0, 0, 0, 0, false
	}
	_, ok = m["Variant"]
	return nbtInt(m["Variant"]), nbtInt(m["MarkVariant"]), nbtInt(m["Color"]), nbtInt(m["Color2"]), ok
}

// MobName returns the name tag of the mob in the bucket, or "".
func (b MobBucket) MobName() string {
	name, _ := b.Data()["NameTag"].(string)
	return name
}

// nbtInt reads an integer NBT value of any width.
func nbtInt(v any) int32 {
	switch v := v.(type) {
	case uint8:
		return int32(v)
	case int8:
		return int32(v)
	case int16:
		return int32(v)
	case int32:
		return v
	case int64:
		return int32(v)
	}
	return 0
}
