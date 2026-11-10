//go:build crossfly_full

package social_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// danceViewer records the entity actions and mob sounds of the world.
type danceViewer struct {
	world.NopViewer
	actions []world.EntityAction
	sounds  []string
}

func (v *danceViewer) ViewEntityAction(_ world.Entity, a world.EntityAction) {
	v.actions = append(v.actions, a)
}

func (v *danceViewer) ViewSound(_ mgl64.Vec3, s world.Sound) {
	if ls, ok := s.(interface {
		LevelSoundEvent() (string, string, bool)
	}); ok {
		ev, _, _ := ls.LevelSoundEvent()
		v.sounds = append(v.sounds, ev)
	}
}

func (v *danceViewer) watch(w *mobtest.World) {
	w.Do(func(tx *world.Tx) {
		l := world.NewLoader(4, tx.World(), v)
		l.Move(tx, mgl64.Vec3{0.5, 1, 0.5})
		l.Load(tx, 1000)
	})
}

// TestAllayDuplicates: a dancing allay given an amethyst shard duplicates itself (love hearts,
// chime.amethyst_block, a new allay, the shard used up); both allays then have the 6000 tick
// cooldown, saved with the world, during which a shard is just given; afterwards it duplicates
// again.
func TestAllayDuplicates(t *testing.T) {
	mobtest.Require(t, "minecraft:interact")
	mobtest.RequireDefinitions(t)
	mobtest.RequireFilters(t)
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon, Persistent: true})
	w.Do(func(tx *world.Tx) { w.Arena(tx, 12, 4) })
	v := &danceViewer{}
	v.watch(w)
	jb := cube.Pos{0, 1, 0}
	w.Do(func(tx *world.Tx) { tx.SetBlock(jb, block.Jukebox{}, nil) })
	p := w.AddPlayer("p", mgl64.Vec3{3.5, 1, 0.5})
	a := w.Spawn("minecraft:allay", mgl64.Vec3{2.5, 2, 0.5})
	w.Tick(2)
	disc := item.NewStack(item.MusicDisc{DiscType: sound.DiscCat()}, 1)
	useJukebox(t, w, p, jb, disc)
	if !dancing(t, w, a) {
		t.Fatal("allay next to the jukebox does not dance")
	}
	shards := item.NewStack(item.AmethystShard{}, 3)
	left, ok := p.Interact(a, shards)
	if !ok || left.Count() != 2 {
		t.Fatalf("giving a shard to the dancing allay: handled %v, %d left, want 2", ok, left.Count())
	}
	if n := len(w.Mobs("minecraft:allay")); n != 2 {
		t.Fatalf("%d allays after the duplication, want 2", n)
	}
	hearts := false
	for _, act := range v.actions {
		if _, ok := act.(core.LoveHeartsAction); ok {
			hearts = true
		}
	}
	chime := false
	for _, s := range v.sounds {
		chime = chime || s == "chime.amethyst_block"
	}
	if !hearts || !chime {
		t.Errorf("duplication showed hearts %v, played chime.amethyst_block %v", hearts, chime)
	}
	a.With(func(m *mob.Mob) {
		if main, _ := m.HeldItems(); !main.Empty() {
			t.Errorf("duplicating allay holds %v", main)
		}
	})

	// The cooldown is saved: after a reload the allay just takes the shard.
	w.Reload()
	useJukebox(t, w, p, jb, item.Stack{}) // the loaded jukebox is silent: take the disc out
	w.Do(func(tx *world.Tx) { mobOf(t, tx, a).Teleport(mgl64.Vec3{2.5, 2, 0.5}) })
	useJukebox(t, w, p, jb, disc)
	if !dancing(t, w, a) {
		t.Fatal("allay does not dance after the reload")
	}
	if _, ok := p.Interact(a, item.NewStack(item.AmethystShard{}, 2)); !ok {
		t.Fatal("allay refused the shard during its cooldown")
	}
	if n := len(w.Mobs("minecraft:allay")); n != 2 {
		t.Fatalf("%d allays after giving a shard during the cooldown, want 2", n)
	}
	a.With(func(m *mob.Mob) {
		if main, _ := m.HeldItems(); main.Empty() {
			t.Error("allay with a cooldown did not take the shard")
		}
		m.SetHeldItems(item.Stack{}, item.Stack{})
	})

	// 6000 ticks later it duplicates again.
	useJukebox(t, w, p, jb, item.Stack{})
	w.Tick(6000)
	w.Do(func(tx *world.Tx) { mobOf(t, tx, a).Teleport(mgl64.Vec3{2.5, 2, 0.5}) })
	useJukebox(t, w, p, jb, disc)
	if _, ok := p.Interact(a, item.NewStack(item.AmethystShard{}, 2)); !ok {
		t.Fatal("allay refused the shard after its cooldown")
	}
	if n := len(w.Mobs("minecraft:allay")); n != 3 {
		t.Fatalf("%d allays after the cooldown, want 3", n)
	}
}

// TestAllayRegenerates: an allay heals 1 health about every 10 ticks (Allay::aiStep: once the
// tick is past the next heal tick, which is then 10 ticks later).
func TestAllayRegenerates(t *testing.T) {
	w := setup(t, mobtest.Options{Time: mobtest.Noon})
	a := w.Spawn("minecraft:allay", mgl64.Vec3{0.5, 2, 0.5})
	w.Tick(2)
	a.With(func(m *mob.Mob) { m.SetHealth(10) })
	w.Tick(33)
	a.With(func(m *mob.Mob) {
		if h := m.Health(); h < 12.5 || h > 13.5 {
			t.Errorf("allay at %.1f health 33 ticks after 10, want 13 (1 per 11 ticks)", h)
		}
	})
}
