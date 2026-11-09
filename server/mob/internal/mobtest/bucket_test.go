//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

// bucketPool is a stone floor at y 0 with a 7x2x7 pool of water around the
// origin (y 1-2) and a stone rim the player stands on.
func bucketPool(w *mobtest.World) {
	item.RegisterMobItems()
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 8, block.Stone{})
		w.Fill(tx, cube.Pos{-5, 1, -5}, cube.Pos{5, 3, 5}, block.Stone{})
		w.Water(tx, cube.Pos{-3, 1, -3}, cube.Pos{3, 3, 3})
	})
}

// mobState is what a mob bucket must keep of its mob.
type mobState struct {
	id              string
	variant, mark   int32
	colour, colour2 uint8
	baby            bool
	health          float64
	name            string
	persists        bool
}

func stateOf(m *mob.Mob) mobState {
	meta := m.Meta()
	return mobState{id: m.Identifier(), variant: meta.Variant, mark: meta.MarkVariant, colour: meta.Color,
		colour2: meta.Color2, baby: m.Baby(), health: m.Health(), name: m.NameTag(), persists: m.Persistent()}
}

func waterBucket() item.Stack {
	it, _ := world.ItemByName("minecraft:water_bucket", 0)
	return item.NewStack(it, 1)
}

// release uses the mob bucket b on the floor of the pool at x, z as the
// player and returns the stack the player keeps and the released mob.
func release(t *testing.T, w *mobtest.World, p *mobtest.Player, b item.Stack, at cube.Pos) (item.Stack, *mob.Mob, mobState) {
	t.Helper()
	before := map[*world.EntityHandle]bool{}
	for _, r := range w.Mobs("") {
		before[r.H()] = true
	}
	var (
		ctx  item.UseContext
		used bool
	)
	p.With(func(pl *player.Player) {
		used = b.Item().(item.UsableOnBlock).UseOnBlock(at.Side(cube.FaceDown), cube.FaceUp, mgl64.Vec3{}, pl.Tx(), pl, &ctx)
	})
	if !used {
		t.Fatalf("%v was not used on the block", b.Item())
	}
	if name, _ := ctx.NewItem.Item().EncodeItem(); name != "minecraft:bucket" || ctx.CountSub != 1 {
		t.Errorf("the bucket left %v (count sub %d), want an empty bucket", ctx.NewItem, ctx.CountSub)
	}
	var (
		got *mob.Mob
		st  mobState
	)
	id := b.Item().(item.MobBucket).Mob
	w.Do(func(tx *world.Tx) {
		if _, ok := tx.Liquid(at); !ok {
			t.Errorf("no water at %v after emptying the bucket", at)
		}
		for e := range tx.Entities() {
			if m, ok := mob.Of(e); ok && !before[e.H()] && m.Identifier() == id {
				got, st = m, stateOf(m)
				if d := m.Position().Sub(mgl64.Vec3{float64(at[0]) + 0.5, float64(at[1]), float64(at[2]) + 0.5}).Len(); d > 1e-6 {
					t.Errorf("released %v %.2f blocks from the middle of %v", id, d, at)
				}
			}
		}
	})
	if got == nil {
		t.Fatalf("no %v came out of the bucket", id)
	}
	return ctx.NewItem, got, st
}

// bucket puts the mob r in a water bucket as the player and returns the
// mob bucket, checking the mob is gone.
func bucket(t *testing.T, w *mobtest.World, p *mobtest.Player, r *mobtest.Ref) item.Stack {
	t.Helper()
	left, ok := p.Interact(r, waterBucket())
	if !ok {
		t.Fatalf("the water bucket did not take the mob:\n%v", w.Dump(r))
	}
	if _, isBucket := left.Item().(item.MobBucket); !isBucket || left.Count() != 1 {
		t.Fatalf("bucketing left %v, want one mob bucket", left)
	}
	if !r.Gone() {
		t.Errorf("the bucketed mob is still in the world")
	}
	return left
}

// diskRoundTrip saves the stack as a player's inventory does and reads it
// back.
func diskRoundTrip(t *testing.T, s item.Stack) item.Stack {
	t.Helper()
	b, err := nbt.MarshalEncoding(item.WriteNBT(s, true), nbt.LittleEndian)
	if err != nil {
		t.Fatalf("encode %v: %v", s, err)
	}
	var m map[string]any
	if err := nbt.UnmarshalEncoding(b, &m, nbt.LittleEndian); err != nil {
		t.Fatal(err)
	}
	return item.ReadNBT(m, nil)
}

// TestBucketMobs: a water bucket used on each bucketable mob (BDS
// BucketableComponent) removes it and gives the matching bucket holding the
// mob's data (BucketItem::addBucketEntitySaveData); the bucket, saved and
// loaded, used on a block places water and lets the same mob out
// (readBucketEntitySaveData), persistent, and turns into an empty bucket.
func TestBucketMobs(t *testing.T) {
	needs(t, true, true, "minecraft:breathable")
	cases := []struct {
		id, bucket string
		opts       mob.SpawnOptions
		setup      func(m *mob.Mob)
		check      func(t *testing.T, data map[string]any, want mobState)
	}{
		{id: "minecraft:cod", bucket: "minecraft:cod_bucket", setup: func(m *mob.Mob) { m.SetHealth(1.5) }},
		{id: "minecraft:salmon", bucket: "minecraft:salmon_bucket", setup: func(m *mob.Mob) { m.SetNameTag("Bob") },
			check: func(t *testing.T, d map[string]any, _ mobState) {
				if d["CustomName"] != "Bob" || d["AppendCustomName"] != uint8(1) {
					t.Errorf("named salmon bucket: CustomName %v AppendCustomName %v", d["CustomName"], d["AppendCustomName"])
				}
			}},
		{id: "minecraft:pufferfish", bucket: "minecraft:pufferfish_bucket"},
		{id: "minecraft:tropicalfish", bucket: "minecraft:tropical_fish_bucket",
			setup: func(m *mob.Mob) { m.Fire("minecraft:become_clownfish", nil) },
			check: func(t *testing.T, d map[string]any, _ mobState) {
				if d["GroupName"] != "item.tropicalSchoolClownfish.name" {
					t.Errorf("clownfish bucket GroupName %v", d["GroupName"])
				}
			}},
		{id: "minecraft:tropicalfish", bucket: "minecraft:tropical_fish_bucket",
			// A random fish (entity_spawned picks any pattern and colours):
			// colour and body names, the body by variant and mark variant,
			// Multi when the colours differ.
			check: func(t *testing.T, d map[string]any, m mobState) {
				if d["GroupName"] != nil {
					t.Skipf("the random fish is a named one (%v)", d["GroupName"])
				}
				patterns := [2][6]string{{"Kob", "Sunstreak", "Snooper", "Dasher", "Brinely", "Spotty"}, {"Flopper", "Stripey", "Glitter", "Blockfish", "Betty", "Clayfish"}}
				colours := [15]string{"White", "Orange", "Magenta", "Sky", "Yellow", "Lime", "Rose", "Gray", "Silver", "Teal", "Plum", "Blue", "Brown", "Green", "Red"}
				body := "item.tropicalBody" + patterns[m.variant][m.mark] + "Single.name"
				var colour2 any
				if m.colour != m.colour2 {
					body = "item.tropicalBody" + patterns[m.variant][m.mark] + "Multi.name"
					colour2 = "item.tropicalColor" + colours[m.colour2] + ".name"
				}
				if d["ColorID"] != "item.tropicalColor"+colours[m.colour]+".name" || d["Color2ID"] != colour2 || d["BodyID"] != body {
					t.Errorf("fish %+v: colour %v colour2 %v body %v, want body %v", m, d["ColorID"], d["Color2ID"], d["BodyID"], body)
				}
			}},
		{id: "minecraft:axolotl", bucket: "minecraft:axolotl_bucket",
			check: func(t *testing.T, d map[string]any, _ mobState) {
				if d["BodyID"] != "item.axolotlAdultBodySingle.name" || d["ColorID"] == nil {
					t.Errorf("axolotl bucket names: colour %v body %v", d["ColorID"], d["BodyID"])
				}
			}},
		{id: "minecraft:axolotl", bucket: "minecraft:axolotl_bucket", opts: mob.SpawnOptions{Event: mob.EventBorn},
			check: func(t *testing.T, d map[string]any, _ mobState) {
				if d["BodyID"] != "item.axolotlBabyBodySingle.name" {
					t.Errorf("baby axolotl bucket body %v", d["BodyID"])
				}
			}},
		{id: "minecraft:tadpole", bucket: "minecraft:tadpole_bucket"},
	}
	for _, c := range cases {
		t.Run(c.bucket, func(t *testing.T) {
			w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
			bucketPool(w)
			p := w.AddPlayer("fisher", mgl64.Vec3{4.5, 4, 0.5})
			r := w.Spawn(c.id, mgl64.Vec3{0.5, 1, 0.5}, c.opts)
			w.Tick(2)
			var want mobState
			r.With(func(m *mob.Mob) {
				if c.setup != nil {
					c.setup(m)
				}
				want = stateOf(m)
			})
			b := bucket(t, w, p, r)
			if name, _ := b.Item().EncodeItem(); name != c.bucket {
				t.Fatalf("bucket %v, want %v", name, c.bucket)
			}
			d := b.Item().(item.MobBucket).Data()
			if d["identifier"] != c.id {
				t.Errorf("bucket data identifier %v", d["identifier"])
			}
			if d["Variant"] != want.variant || d["MarkVariant"] != want.mark || d["Color"] != want.colour || d["Color2"] != want.colour2 {
				t.Errorf("bucket data Variant %v MarkVariant %v Color %v Color2 %v, mob had %+v", d["Variant"], d["MarkVariant"], d["Color"], d["Color2"], want)
			}
			if c.check != nil {
				c.check(t, d, want)
			}
			b = diskRoundTrip(t, b)
			_, _, got := release(t, w, p, b, cube.Pos{-1, 1, -1})
			want.persists = true
			if got != want {
				t.Errorf("released mob %+v, bucketed %+v", got, want)
			}
			// It lives on: a tick later it is still there and the same.
			w.Tick(5)
			if n := len(w.Mobs(c.id)); n != 1 {
				t.Errorf("%d %v after releasing, want 1", n, c.id)
			}
		})
	}
}

// TestEmptyMobBucket: a mob bucket without data (the creative inventory's)
// lets a new mob of its kind out, which never despawns.
func TestEmptyMobBucket(t *testing.T) {
	needs(t, true, true)
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	bucketPool(w)
	p := w.AddPlayer("fisher", mgl64.Vec3{4.5, 4, 0.5})
	b, ok := item.NewMobBucket("minecraft:tropicalfish", nil)
	if !ok {
		t.Fatal("no tropical fish bucket")
	}
	_, _, got := release(t, w, p, item.NewStack(b, 1), cube.Pos{0, 1, 0})
	if !got.persists {
		t.Errorf("a mob from a bucket despawns")
	}
}

// TestNotBucketable: other mobs and other buckets do nothing.
func TestNotBucketable(t *testing.T) {
	needs(t, true, true)
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	bucketPool(w)
	p := w.AddPlayer("fisher", mgl64.Vec3{4.5, 4, 0.5})
	sq := w.Spawn("minecraft:squid", mgl64.Vec3{0.5, 1, 0.5})
	if _, ok := p.Interact(sq, waterBucket()); ok {
		t.Errorf("a squid went into a water bucket")
	}
	cod := w.Spawn("minecraft:cod", mgl64.Vec3{-1.5, 1, 0.5})
	if _, ok := p.Interact(cod, item.NewStack(item.Bucket{}, 1)); ok {
		t.Errorf("an empty bucket took a cod")
	}
	if cod.Gone() {
		t.Errorf("the cod is gone")
	}
}

// TestAxolotlTropicalFishBucket: the tropical_fish_bucket of axolotl.json
// (tempt items, breed items with result_item water_bucket:0) resolves to the
// mob bucket: holding one tempts an axolotl, feeding two adults one each
// leaves water buckets and makes a baby.
func TestAxolotlTropicalFishBucket(t *testing.T) {
	needs(t, true, true, "minecraft:behavior.tempt", "minecraft:breedable", "minecraft:behavior.breed")
	item.RegisterMobItems()
	s, ok := core.ParseItemDescriptor("tropical_fish_bucket").Stack(1)
	if _, isBucket := s.Item().(item.MobBucket); !ok || !isBucket {
		t.Fatalf("tropical_fish_bucket resolves to %v (%v)", s, ok)
	}
	for _, name := range []string{"cod_bucket", "salmon_bucket", "pufferfish_bucket", "axolotl_bucket", "tadpole_bucket", "bucket:4", "bucket:12"} {
		if s, ok := core.ParseItemDescriptor(name).Stack(1); !ok {
			t.Errorf("%v resolves to no item", name)
		} else if _, isBucket := s.Item().(item.MobBucket); !isBucket {
			t.Errorf("%v resolves to %v", name, s.Item())
		}
	}
	fish, _ := item.NewMobBucket("minecraft:tropicalfish", nil)

	t.Run("tempt", func(t *testing.T) {
		w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
		bucketPool(w)
		p := w.AddPlayer("keeper", mgl64.Vec3{4.5, 4, 0.5})
		p.Hold(item.NewStack(fish, 1))
		a := w.Spawn("minecraft:axolotl", mgl64.Vec3{-2.5, 1, 0.5})
		n, ok := w.TickUntil(200, func(tx *world.Tx) bool {
			m, ok := a.Mob(tx)
			return ok && m.Goals().Running("minecraft:behavior.tempt")
		})
		if !ok {
			t.Fatalf("a tropical fish bucket did not tempt the axolotl:\n%v", w.Dump(a))
		}
		t.Logf("tempted after %d ticks", n)
	})
	t.Run("breed", func(t *testing.T) {
		w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
		bucketPool(w)
		p := w.AddPlayer("keeper", mgl64.Vec3{4.5, 4, 0.5})
		pair := []*mobtest.Ref{
			w.Spawn("minecraft:axolotl", mgl64.Vec3{-0.5, 1, 0.5}),
			w.Spawn("minecraft:axolotl", mgl64.Vec3{1.5, 1, 0.5}),
		}
		for i, a := range pair {
			left, ok := p.Interact(a, item.NewStack(fish, 1))
			if !ok {
				t.Fatalf("axolotl %d did not take the tropical fish bucket:\n%v", i, w.Dump(a))
			}
			if name, meta := left.Item().EncodeItem(); name != "minecraft:water_bucket" || meta != 0 || left.Count() != 1 {
				t.Errorf("feeding axolotl %d left %v, want a water bucket", i, left)
			}
		}
		n, ok := w.TickUntil(600, func(tx *world.Tx) bool {
			for e := range tx.Entities() {
				if m, ok := mob.Of(e); ok && m.Identifier() == "minecraft:axolotl" && m.Baby() {
					return true
				}
			}
			return false
		})
		if !ok {
			t.Fatalf("the fed axolotls made no baby:\n%v\n%v", w.Dump(pair[0]), w.Dump(pair[1]))
		}
		t.Logf("baby after %d ticks", n)
	})
}
