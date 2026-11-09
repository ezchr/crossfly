//go:build crossfly_full

package javasession

import (
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
)

// TestMerchantWindowSurvivesMenuCheck: the periodic window check (checkMenu, run from
// HandleInventories) used to dereference the nil inventory of the virtual merchant window and
// crash the server the first time a Java player opened a villager.
func TestMerchantWindowSurvivesMenuCheck(t *testing.T) {
	s, st, c := itemsTestPlayer(world.GameModeSurvival)
	w := itemsTestWorld(t)
	itemsDo(t, w, func(tx *world.Tx) {
		st.tx = tx
		s.OpenTradeWindow(tx, nil, session.TradeWindow{Name: "Librarian"}, nil)
		m := st.open.Load()
		if m == nil || m.kind != menuMerchant {
			t.Fatalf("merchant window not open: %+v", m)
		}
		m.lastCheck = time.Time{}
		s.checkMenu(tx, c)
		if st.open.Load() != m {
			t.Fatal("merchant window closed by the menu check")
		}
	})
}

// TestMerchantTitleTranslatesBedrockKeys: the villager files name the window with Bedrock language
// keys, which a Java client would print raw ("entity.villager.librarian").
func TestMerchantTitleTranslatesBedrockKeys(t *testing.T) {
	if c := merchantTitle("entity.villager.librarian"); c.Translate != "entity.minecraft.villager.librarian" {
		t.Errorf("librarian title %+v", c)
	}
	if c := merchantTitle("entity.villager.armor"); c.Translate != "entity.minecraft.villager.armorer" {
		t.Errorf("armorer title %+v", c)
	}
	if c := merchantTitle("Shop"); c.Translate != "" || c.Text != "Shop" {
		t.Errorf("plain title %+v", c)
	}
}

// TestJavaBossBarRemovedWithEntity: a Java boss bar outlives its entity unless it is removed by
// hand, so hiding a boss entity from the session removes the bar it showed.
func TestJavaBossBarRemovedWithEntity(t *testing.T) {
	s, st, _ := itemsTestPlayer(world.GameModeSurvival)
	w := itemsTestWorld(t)
	itemsDo(t, w, func(tx *world.Tx) {
		st.tx = tx
		e := tx.AddEntity(world.EntitySpawnOpts{}.New(mountEntType{}, mountConf{&mountBehaviour{}}))
		if s.entityIDs == nil {
			s.entityIDs = map[*world.EntityHandle]int32{}
		}
		s.entityIDs[e.H()] = 9
		s.ShowMobBossBar(e, "Boss", 1, 5, false)
		itemsPackets(s)
		s.HideEntity(e)
		if n := itemsCount(itemsPackets(s), v777.ClientboundPlayBossEvent); n != 1 {
			t.Fatalf("%d boss events when the boss was hidden, want 1 (remove)", n)
		}
		s.HideEntity(e)
		if n := itemsCount(itemsPackets(s), v777.ClientboundPlayBossEvent); n != 0 {
			t.Fatalf("bar removed twice")
		}
	})
}
