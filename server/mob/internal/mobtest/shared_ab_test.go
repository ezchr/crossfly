//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// ambientViewer records the mob ambient sounds it is shown.
type ambientViewer struct {
	world.NopViewer
	sounds []ambientHeard
}

type ambientHeard struct {
	event, typ       string
	baby, clientSide bool
}

func (v *ambientViewer) ViewSound(_ mgl64.Vec3, s world.Sound) {
	ls, ok := s.(interface {
		LevelSoundEvent() (string, string, bool)
	})
	if !ok {
		return
	}
	h := ambientHeard{}
	h.event, h.typ, h.baby = ls.LevelSoundEvent()
	if cs, ok := s.(interface{ BedrockClientSide() bool }); ok {
		h.clientSide = cs.BedrockClientSide()
	}
	v.sounds = append(v.sounds, h)
}

// ambientRun ticks the mobs spawn adds for 400 s in a walled arena and
// returns how often each "<type> <event>" ambient sound was heard. Every
// one must be ClientSide (only Java gets it).
func ambientRun(t *testing.T, spawn func(w *mobtest.World)) map[string]int {
	t.Helper()
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Arena(tx, 8, 4) })
	v := &ambientViewer{}
	w.Do(func(tx *world.Tx) {
		l := world.NewLoader(4, tx.World(), v)
		l.Move(tx, mgl64.Vec3{0.5, 1, 0.5})
		l.Load(tx, 1000)
	})
	spawn(w)
	v.sounds = nil
	w.Tick(8000)
	n := map[string]int{}
	for _, s := range v.sounds {
		if s.event != "ambient" && s.event != "ambient.baby" && s.event != "ambient.screamer" {
			continue
		}
		if !s.clientSide {
			t.Errorf("%v %v sent to Bedrock too (not ClientSide)", s.typ, s.event)
		}
		n[s.typ+" "+s.event]++
	}
	return n
}

// TestHardcodedAmbientSounds: cows (no ambient_sound_interval) moo every
// 12 + random(16) s (Animal::reloadHardcoded + client defaults: 20 s on
// average), always as sounds the Bedrock client plays by itself
// (ClientSide), so only Java gets them; a screaming goat uses
// ambient.screamer, a baby sheep ambient.baby. (The goat gets its own
// arena: screaming goats ram other mobs.)
func TestHardcodedAmbientSounds(t *testing.T) {
	needs(t, true, false)
	n := ambientRun(t, func(w *mobtest.World) {
		for i := range 10 {
			w.Spawn("minecraft:cow", mgl64.Vec3{float64(i) - 4.5, 1, -3.5})
		}
	})
	// Expected 10 cows * 400 s / 20 s = 200.
	if c := n["minecraft:cow ambient"] + n["minecraft:cow ambient.baby"]; c < 150 || c > 260 {
		t.Errorf("cows mooed %v times in 400 s, want about 200 (all: %v)", c, n)
	}
	n = ambientRun(t, func(w *mobtest.World) {
		goat := w.Spawn("minecraft:goat", mgl64.Vec3{0.5, 1, 4.5})
		w.Tick(2) // genetics picks born_default/born_screamer first
		goat.With(func(m *mob.Mob) {
			m.Fire("minecraft:born_screamer", m.Ctx())
			if m.Meta().Variant != 1 {
				t.Fatalf("goat variant %v after born_screamer", m.Meta().Variant)
			}
		})
	})
	if c := n["minecraft:goat ambient.screamer"]; c < 8 || n["minecraft:goat ambient"] != 0 {
		t.Errorf("screaming goat sounds: %v", n)
	}
	n = ambientRun(t, func(w *mobtest.World) {
		w.Spawn("minecraft:sheep", mgl64.Vec3{4.5, 1, 4.5}, mob.SpawnOptions{Event: "minecraft:entity_born"})
	})
	if c := n["minecraft:sheep ambient.baby"]; c < 8 || n["minecraft:sheep ambient"] != 0 {
		t.Errorf("baby sheep sounds: %v", n)
	}
}

// pigAngerSetup: an adult piglin and a player in gold armour it ignores.
func pigAngerSetup(t *testing.T) (*mobtest.World, *mobtest.Ref, *mobtest.Player, func(tx *world.Tx) bool) {
	needs(t, true, true, append(walkers, "minecraft:block_sensor", "minecraft:behavior.nearest_attackable_target")...)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 16, block.Stone{}) })
	pg := w.Spawn("minecraft:piglin", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "spawn_adult"})
	p := w.AddPlayer("p", mgl64.Vec3{4.5, 1, 0.5})
	p.With(func(pl *player.Player) {
		pl.Armour().SetHelmet(item.NewStack(item.Helmet{Tier: item.ArmourTierGold{}}, 1))
	})
	targets := func(tx *world.Tx) bool {
		m, _ := pg.Mob(tx)
		tg, ok := m.Target()
		return ok && tg.H() == p.H()
	}
	if _, ok := w.TickUntil(40, targets); ok {
		t.Fatal("piglin targets a player in gold armour before any block broke")
	}
	return w, pg, p, targets
}

// TestPiglinAngryAtPlayerMinedGold: a player breaking a gold block (the
// real Player.BreakBlock path, survival) near a piglin angers it, and so
// does breaking a block of stone first not.
func TestPiglinAngryAtPlayerMinedGold(t *testing.T) {
	w, pg, p, targets := pigAngerSetup(t)
	w.Do(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{3, 1, 1}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{3, 1, 2}, block.Gold{}, nil)
	})
	p.With(func(pl *player.Player) { pl.BreakBlock(cube.Pos{3, 1, 1}) })
	if _, ok := w.TickUntil(40, targets); ok {
		t.Fatal("piglin angry after the player broke stone")
	}
	p.With(func(pl *player.Player) { pl.BreakBlock(cube.Pos{3, 1, 2}) })
	w.Do(func(tx *world.Tx) {
		if _, air := tx.Block(cube.Pos{3, 1, 2}).(block.Air); !air {
			t.Fatalf("the player did not break the gold block")
		}
	})
	if n, ok := w.TickUntil(60, targets); !ok {
		t.Fatalf("piglin not angry %d ticks after the player broke a gold block:\n%v", n, w.Dump(pg))
	}
}

// TestPiglinAngryAtCreativeMinedGold: creative breaking goes through the
// same path.
func TestPiglinAngryAtCreativeMinedGold(t *testing.T) {
	w, pg, p, targets := pigAngerSetup(t)
	w.Do(func(tx *world.Tx) { tx.SetBlock(cube.Pos{3, 1, 2}, block.Gold{}, nil) })
	p.With(func(pl *player.Player) {
		pl.SetGameMode(world.GameModeCreative)
		pl.BreakBlock(cube.Pos{3, 1, 2})
		pl.SetGameMode(world.GameModeSurvival)
	})
	if n, ok := w.TickUntil(60, targets); !ok {
		t.Fatalf("piglin not angry %d ticks after a creative player broke a gold block:\n%v", n, w.Dump(pg))
	}
}

// TestPiglinAngryAtExplodedChest: an explosion that blows up a chest near
// a piglin angers it (BDS dispatches every block change of the blast);
// one that only breaks stone does not.
func TestPiglinAngryAtExplodedChest(t *testing.T) {
	w, pg, _, targets := pigAngerSetup(t)
	w.Do(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{-9, 1, 0}, block.Stone{}, nil)
		block.ExplosionConfig{ItemDropChance: -1}.Explode(tx, world.BlockExplosionSource{Pos: cube.Pos{-9, 1, 1}, ExplosionSize: 3})
	})
	if _, ok := w.TickUntil(40, targets); ok {
		t.Fatal("piglin angry after a blast that broke no listed block")
	}
	w.Do(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{-9, 1, 0}, block.NewChest(), nil)
		block.ExplosionConfig{ItemDropChance: -1}.Explode(tx, world.BlockExplosionSource{Pos: cube.Pos{-9, 1, 1}, ExplosionSize: 3})
		if _, air := tx.Block(cube.Pos{-9, 1, 0}).(block.Air); !air {
			t.Fatalf("the blast did not break the chest: %#v", tx.Block(cube.Pos{-9, 1, 0}))
		}
	})
	if n, ok := w.TickUntil(60, targets); !ok {
		t.Fatalf("piglin not angry %d ticks after a blast broke a chest:\n%v", n, w.Dump(pg))
	}
}
