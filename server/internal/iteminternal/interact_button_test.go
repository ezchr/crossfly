package iteminternal

import (
	"reflect"
	"testing"
)

func TestNormaliseInteractButton(t *testing.T) {
	want := func(text string) map[string]any {
		return map[string]any{"interact_text": text, "requires_interact": uint8(1)}
	}
	for _, c := range []struct {
		in   any
		want any
	}{
		{true, want("action.interact.use")},
		{"Ride", want("Ride")},
		{map[string]any{"interact_text": "x", "requires_interact": uint8(1)}, map[string]any{"interact_text": "x", "requires_interact": uint8(1)}},
	} {
		b := &ComponentBuilder{components: map[string]any{"minecraft:interact_button": c.in}}
		b.normaliseInteractButton()
		if got := b.components["minecraft:interact_button"]; !reflect.DeepEqual(got, c.want) {
			t.Errorf("%v: got %v, want %v", c.in, got, c.want)
		}
	}
	b := &ComponentBuilder{components: map[string]any{"minecraft:interact_button": false}}
	b.normaliseInteractButton()
	if _, ok := b.components["minecraft:interact_button"]; ok {
		t.Error("false: the component should be dropped")
	}
}
