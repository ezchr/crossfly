//go:build crossfly_full

package javasession

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	jitem "github.com/df-mc/dragonfly/server/java/protocol/item"
)

// item.MobBucket is what mobBucketComponents reads.
var _ mobBucket = item.MobBucket{}

// TestJavaStackMobBucket: javaStack converts a real tropical fish bucket with its fish's data.
func TestJavaStackMobBucket(t *testing.T) {
	b, ok := item.NewMobBucket("minecraft:tropicalfish", map[string]any{
		"identifier": "minecraft:tropicalfish", "Variant": int32(0), "MarkVariant": int32(0), "Color": uint8(1), "Color2": uint8(0),
	})
	if !ok {
		t.Fatal("no tropical fish bucket")
	}
	var js jitem.Stack
	javaStack(item.NewStack(b, 1), &js)
	if want := mustJavaItem("minecraft:tropical_fish_bucket"); js.ID != want {
		t.Errorf("Java item %d, want %d", js.ID, want)
	}
	if got, ok := rawVarInt(t, &js, jitem.CompTropicalFishBaseColor); !ok || got != 1 {
		t.Errorf("clownfish base colour %d (%v), want orange 1", got, ok)
	}
}
