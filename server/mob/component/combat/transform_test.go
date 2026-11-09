//go:build crossfly_full

package combat

import (
	"encoding/json"
	"slices"
	"testing"
)

// The zombie villager cure (zombie_villager_v2 "to_villager"): every time
// is (int)(seconds*20), block_max defaults to block_radius
// (TransformationDescription::deserializeData).
func TestTransformationParsesCureDelay(t *testing.T) {
	js := `{"begin_transform_sound":"remedy","keep_level":true,"delay":{"block_assist_chance":0.01,"block_radius":4,"block_chance":0.3,"range_max":200,"block_types":["minecraft:bed","iron_bars"],"range_min":80,"value":100},"drop_equipment":true,"into":"minecraft:villager_v2","transformation_sound":"unfect"}`
	c, err := newTransformation("minecraft:transformation", json.RawMessage(js))
	if err != nil {
		t.Fatal(err)
	}
	tr := c.(*Transformation)
	if tr.delay != 2000 || tr.rangeMin != 1600 || tr.rangeMax != 4000 {
		t.Errorf("delay %d range %d..%d, want 2000 1600..4000", tr.delay, tr.rangeMin, tr.rangeMax)
	}
	if tr.assistChance != 0.01 || tr.blockRadius != 4 || tr.blockMax != 4 || tr.blockChance != 0.3 {
		t.Errorf("block assist %v r%d max%d chance %v", tr.assistChance, tr.blockRadius, tr.blockMax, tr.blockChance)
	}
	if !slices.Equal(tr.blockTypes, []string{"minecraft:bed", "minecraft:iron_bars"}) {
		t.Errorf("block types %v", tr.blockTypes)
	}
	if !slices.Equal(tr.beginSounds, []string{"remedy"}) || !slices.Equal(tr.sounds, []string{"unfect"}) {
		t.Errorf("sounds %v %v", tr.beginSounds, tr.sounds)
	}
	if !tr.keepLevel || !tr.dropEquipment || tr.into != "minecraft:villager_v2" {
		t.Errorf("flags %+v", tr)
	}
	// Before Attach rolls it: value + range_min.
	if tr.Left() != 3600 {
		t.Errorf("left %d, want 3600", tr.Left())
	}
}

func TestTransformationParsesNumberDelayAndSoundList(t *testing.T) {
	c, err := newTransformation("minecraft:transformation", json.RawMessage(`{"delay":0.5,"into":"minecraft:witch","transformation_sound":["a","b"],"drop_inventory":true,"block_max":3}`))
	if err != nil {
		t.Fatal(err)
	}
	tr := c.(*Transformation)
	if tr.delay != 10 || tr.rangeMin != 0 || tr.rangeMax != 0 || tr.assistChance != 0 {
		t.Errorf("delay %d range %d..%d assist %v", tr.delay, tr.rangeMin, tr.rangeMax, tr.assistChance)
	}
	if !slices.Equal(tr.sounds, []string{"a", "b"}) || !tr.dropInventory {
		t.Errorf("sounds %v drop_inventory %v", tr.sounds, tr.dropInventory)
	}
	c, err = newTransformation("minecraft:transformation", json.RawMessage(`{"into":"minecraft:cow","delay":{"value":1,"block_radius":2,"block_max":7}}`))
	if err != nil {
		t.Fatal(err)
	}
	if tr := c.(*Transformation); tr.blockMax != 7 || tr.delay != 20 {
		t.Errorf("block_max %d delay %d", tr.blockMax, tr.delay)
	}
}
