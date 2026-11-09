//go:build crossfly_full

package core_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/component/core"
)

// TestColourSplitNames: a colour-split legacy name without data ("carpet",
// "wool") matches every colour and makes the white one; moss carpet is no
// carpet.
func TestColourSplitNames(t *testing.T) {
	carpet := core.ParseItemDescriptor("carpet")
	if !carpet.Matches(item.NewStack(block.Carpet{Colour: item.ColourRed()}, 1)) {
		t.Errorf("carpet does not match a red carpet")
	}
	if carpet.Matches(item.NewStack(block.MossCarpet{}, 1)) {
		t.Errorf("carpet matches a moss carpet")
	}
	s, ok := core.ParseItemDescriptor("wool").Stack(1)
	if name, _ := s.Item().EncodeItem(); !ok || name != "minecraft:white_wool" {
		t.Errorf("wool makes %v, want white wool", s)
	}
}
