package javasession

import (
	"encoding/binary"
	"testing"

	jitem "github.com/df-mc/dragonfly/server/java/protocol/item"
)

// fakeMobBucket stands in for item.MobBucket (Crossfly Full only).
type fakeMobBucket struct {
	name           string
	v, mark, c, c2 int32
	hasVariants    bool
	mobName        string
}

func (b fakeMobBucket) EncodeItem() (string, int16) { return b.name, 0 }
func (b fakeMobBucket) Variants() (int32, int32, int32, int32, bool) {
	return b.v, b.mark, b.c, b.c2, b.hasVariants
}
func (b fakeMobBucket) MobName() string { return b.mobName }

func rawVarInt(t *testing.T, js *jitem.Stack, typ int32) (int32, bool) {
	t.Helper()
	for _, r := range js.Raw {
		if r.Type == typ {
			v, n := binary.Uvarint(r.Data)
			if n != len(r.Data) {
				t.Fatalf("component %d: bad varint % x", typ, r.Data)
			}
			return int32(v), true
		}
	}
	return 0, false
}

// TestMobBucketComponents: a tropical fish bucket gets Java's pattern (base | index<<8) and dye colours from the
// Bedrock variant, mark variant and colours; an axolotl bucket the Java variant; a named mob's bucket its name.
func TestMobBucketComponents(t *testing.T) {
	var js jitem.Stack
	// Bedrock variant 1 (large), mark 2: Java GLITTER (1 | 2<<8); lime on light blue.
	b := fakeMobBucket{name: "minecraft:tropical_fish_bucket", v: 1, mark: 2, c: 5, c2: 3, hasVariants: true, mobName: "Nemo"}
	mobBucketComponents(b, b.name, &js)
	for typ, want := range map[int32]int32{jitem.CompTropicalFishPattern: 1 | 2<<8, jitem.CompTropicalFishBaseColor: 5, jitem.CompTropicalFishPatternColor: 3} {
		if got, ok := rawVarInt(t, &js, typ); !ok || got != want {
			t.Errorf("component %d = %d (%v), want %d", typ, got, ok, want)
		}
	}
	if !js.Has(jitem.CompCustomName) || js.CustomName.Text != "Nemo" {
		t.Errorf("named fish bucket custom name %+v", js.CustomName)
	}

	js.Reset()
	b = fakeMobBucket{name: "minecraft:axolotl_bucket", v: 1, hasVariants: true} // Bedrock cyan
	mobBucketComponents(b, b.name, &js)
	if got, ok := rawVarInt(t, &js, jitem.CompAxolotlVariant); !ok || got != 3 {
		t.Errorf("cyan axolotl bucket variant %d (%v), want Java cyan 3", got, ok)
	}
	if js.Has(jitem.CompCustomName) {
		t.Errorf("unnamed axolotl bucket got a custom name")
	}

	js.Reset()
	b = fakeMobBucket{name: "minecraft:tropical_fish_bucket"} // from the creative inventory
	mobBucketComponents(b, b.name, &js)
	if len(js.Raw) != 0 {
		t.Errorf("a bucket without a mob got components %v", js.Raw)
	}
}

// TestMobBucketSounds: Bedrock's one mob bucket sound plays as the Java bucket sound of the mob.
func TestMobBucketSounds(t *testing.T) {
	for _, c := range []struct{ event, typ, want string }{
		{"bucket.fill.fish", "minecraft:cod", "minecraft:item.bucket.fill_fish"},
		{"bucket.fill.fish", "minecraft:tropicalfish", "minecraft:item.bucket.fill_fish"},
		{"bucket.fill.fish", "minecraft:axolotl", "minecraft:item.bucket.fill_axolotl"},
		{"bucket.empty.fish", "minecraft:tadpole", "minecraft:item.bucket.empty_tadpole"},
		{"bucket.empty.fish", "minecraft:salmon", "minecraft:item.bucket.empty_fish"},
	} {
		got, ok := javaMobSoundName(c.event, c.typ)
		if !ok || got != c.want || !validSoundName(got) {
			t.Errorf("%v of %v: %q (%v), want %q", c.event, c.typ, got, ok, c.want)
		}
	}
}
