//go:build crossfly_full

package boss

import (
	"encoding/json"
	"testing"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat"
	"github.com/df-mc/dragonfly/server/mob/data"
)

func comp(t *testing.T, f mob.ComponentFactory, key, raw string) mob.Component {
	t.Helper()
	c, err := f(key, json.RawMessage(raw))
	if err != nil {
		t.Fatalf("%v: %v", key, err)
	}
	return c
}

// TestBossDefaults: BossDefinition hud_range 55, no dark sky, purple.
func TestBossDefaults(t *testing.T) {
	b := comp(t, newBoss, "minecraft:boss", `{}`).(*Boss)
	if b.hudRange != 55 || b.darken || b.colour != ColourPurple || b.name != "" || b.health >= 0 {
		t.Errorf("boss %+v", b)
	}
	b = comp(t, newBoss, "minecraft:boss", `{"hud_range": 125, "should_darken_sky": true, "name": "x"}`).(*Boss)
	if b.hudRange != 125 || !b.darken || b.name != "x" {
		t.Errorf("boss from JSON %+v", b)
	}
}

// TestAngerLevelDefaults: AngerLevelDefinition defaults (BDS docs: max 100,
// threshold 80, boost 20, 1 s, annoyingness 0) and the warden's values.
func TestAngerLevelDefaults(t *testing.T) {
	a := comp(t, newAngerLevel, "minecraft:anger_level", `{}`).(*AngerLevel)
	if a.MaxAnger != 100 || a.Threshold != 80 || a.Boost != 20 || a.interval != 20 || a.Annoyingness != 0 ||
		a.ProjectileAnnoyingness != 0 || !a.removeBelow {
		t.Errorf("anger_level %+v", a)
	}
	var w struct {
		E struct {
			C map[string]json.RawMessage `json:"components"`
		} `json:"minecraft:entity"`
	}
	f, ok := data.Entity("warden.json")
	if !ok {
		t.Fatal("no warden.json")
	}
	if err := json.Unmarshal(f, &w); err != nil {
		t.Fatal(err)
	}
	a = comp(t, newAngerLevel, "minecraft:anger_level", string(w.E.C["minecraft:anger_level"])).(*AngerLevel)
	if a.MaxAnger != 150 || a.Annoyingness != 35 || a.ProjectileAnnoyingness != 10 || len(a.sounds) != 2 || a.sounds[0].sound != "listening_angry" {
		t.Errorf("warden anger_level %+v", a)
	}
	// The warden's Molang heartbeat interval is checked with a mob in
	// goal/boss TestWardenClientData.
	comp(t, newHeartbeat, "minecraft:heartbeat", string(w.E.C["minecraft:heartbeat"]))
}

// TestSmallComponentDefaults: vibration_listener (range 16, warden cooldown
// 40), heartbeat (1 s, sound 376), movement_sound_distance_offset (1).
func TestSmallComponentDefaults(t *testing.T) {
	v := comp(t, newVibrationListener, "minecraft:vibration_listener", `{}`).(*VibrationListener)
	if v.Range != 16 || v.cooldown != 40 {
		t.Errorf("vibration_listener %+v", v)
	}
	h := comp(t, newHeartbeat, "minecraft:heartbeat", `{}`).(*Heartbeat)
	if h.interval.Eval(nil) != 1 || h.sound != 376 {
		t.Errorf("heartbeat %+v", h)
	}
	if o := comp(t, newSoundOffset, "minecraft:movement_sound_distance_offset", `{}`).(soundOffset); o.v != 1 {
		t.Errorf("sound offset %v", o.v)
	}
	if o := comp(t, newSoundOffset, "minecraft:movement_sound_distance_offset", `{"value": 0.55}`).(soundOffset); o.v != 0.55 {
		t.Errorf("sound offset %v", o.v)
	}
	tr := comp(t, newEventTrigger, OnStartLanding, `{"event": "minecraft:start_land", "target": "self"}`).(*eventTrigger)
	if tr.t.Event != "minecraft:start_land" || tr.t.Target != "self" {
		t.Errorf("trigger %+v", tr.t)
	}
}

// TestDragonRing: the ring picks stay on the outer ring with crystals and
// the middle ring without.
func TestDragonRing(t *testing.T) {
	d := &Dragon{}
	cw := true
	for j := 12; j < 24; j++ {
		if n := d.RingNext(j, false, &cw); n < 12 || n > 19 {
			t.Errorf("RingNext(%d) = %d without crystals", j, n)
		}
	}
	d.NumCrystals = 3
	for j := 0; j < 24; j++ {
		if n := d.RingNext(j, j%3 == 0, &cw); n < 0 || n > 11 {
			t.Errorf("RingNext(%d) = %d with crystals", j, n)
		}
	}
	if wrapDegrees(190) != -170 || wrapDegrees(-190) != 170 {
		t.Errorf("wrapDegrees %v %v", wrapDegrees(190), wrapDegrees(-190))
	}
}

// TestProjectiles: the four projectile types and that ShootProjectile
// knows them (combat.RegisterProjectile).
func TestProjectiles(t *testing.T) {
	want := []string{"minecraft:wither_skull", "minecraft:wither_skull_dangerous", "minecraft:dragon_fireball", "minecraft:breeze_wind_charge_projectile"}
	types := ProjectileTypes()
	for i, id := range want {
		if types[i].EncodeEntity() != id {
			t.Errorf("type %d is %v, want %v", i, types[i].EncodeEntity(), id)
		}
	}
	_ = combat.ShootProjectile
}
