package javasession

import (
	"encoding/binary"

	jitem "github.com/df-mc/dragonfly/server/java/protocol/item"
	"github.com/df-mc/dragonfly/server/world"
)

// mobBucket is a bucket holding a mob (item.MobBucket, Crossfly Full only).
type mobBucket interface {
	// Variants returns the Bedrock variant, mark variant and colours of the mob in the bucket.
	Variants() (variant, markVariant, colour, colour2 int32, ok bool)
	// MobName returns the name tag of the mob in the bucket.
	MobName() string
}

// javaAxolotlVariants are the Java axolotl variants (Axolotl.Variant ids) of the Bedrock ones (minecraft:variant:
// lucy, cyan, gold, wild, blue).
var javaAxolotlVariants = [...]int32{0, 3, 2, 1, 4}

// mobBucketComponents adds the components Java Edition puts on a mob bucket (Bucketable.saveDefaultDataToBucketTag
// and the mobs' saveToBucketTag): the mob's name as the item's, and the pattern and colours of a tropical fish
// (the client names the fish from them) or the variant of an axolotl. The Bedrock tropical fish's variant and
// mark variant are Java's pattern base and index, its colours Java's dye ids.
func mobBucketComponents(it world.Item, name string, js *jitem.Stack) {
	b, ok := it.(mobBucket)
	if !ok {
		return
	}
	if n := b.MobName(); n != "" && !js.Has(jitem.CompCustomName) {
		js.CustomName = itemText(n)
		js.Add(jitem.CompCustomName)
	}
	v, mark, c, c2, ok := b.Variants()
	if !ok {
		return
	}
	switch name {
	case "minecraft:tropical_fish_bucket":
		js.Raw = append(js.Raw,
			jitem.RawComponent{Type: jitem.CompTropicalFishPattern, Data: binary.AppendUvarint(nil, uint64(uint32(v&1|(mark&0xff)<<8)))},
			jitem.RawComponent{Type: jitem.CompTropicalFishBaseColor, Data: binary.AppendUvarint(nil, uint64(uint32(c&15)))},
			jitem.RawComponent{Type: jitem.CompTropicalFishPatternColor, Data: binary.AppendUvarint(nil, uint64(uint32(c2&15)))})
	case "minecraft:axolotl_bucket":
		if v >= 0 && int(v) < len(javaAxolotlVariants) {
			js.Raw = append(js.Raw, jitem.RawComponent{Type: jitem.CompAxolotlVariant, Data: binary.AppendUvarint(nil, uint64(javaAxolotlVariants[v]))})
		}
	}
}
