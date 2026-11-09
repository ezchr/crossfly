//go:build crossfly_full

package hostile_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	gv "github.com/df-mc/dragonfly/server/mob/goal/village"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// TestEndermanTeleportsOnMeleeHit: EnderMan::_hurt and hurtEffects teleport
// the enderman when a player hits it in melee, and the hit still lands.
func TestEndermanTeleportsOnMeleeHit(t *testing.T) {
	w := arena(t, mobtest.Options{Time: mobtest.Noon}, 24)
	e := w.Spawn("minecraft:enderman", mgl64.Vec3{0.5, 1, 0.5})
	p := w.AddPlayer("fighter", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(1)
	moved := false
	for i := 0; i < 5 && !moved; i++ {
		w.Do(func(tx *world.Tx) {
			m, _ := e.Mob(tx)
			pl, _ := p.H().Entity(tx)
			m.SetHealth(m.MaxHealth())
			before, hp := m.Position(), m.Health()
			m.Hurt(2, entity.AttackDamageSource{Attacker: pl})
			if m.Health() >= hp {
				t.Errorf("melee hit did no damage")
			}
			moved = m.Position().Sub(before).Len() > 1
		})
		w.Tick(11)
	}
	if !moved {
		t.Fatalf("enderman never teleported away from a melee hit:\n%v", w.Dump(e))
	}
}

// TestEndermanDodgesWithoutRoom: a projectile hit is cancelled even when
// every teleport fails (an enderman on a lone pillar over the void).
func TestEndermanDodgesWithoutRoom(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	w.Do(func(tx *world.Tx) { tx.SetBlock(cube.Pos{0, 0, 0}, block.Stone{}, nil) })
	e := w.Spawn("minecraft:enderman", mgl64.Vec3{0.5, 1, 0.5})
	p := w.AddPlayer("shooter", mgl64.Vec3{8.5, 1, 0.5})
	w.Do(func(tx *world.Tx) {
		m, _ := e.Mob(tx)
		pl, _ := p.H().Entity(tx)
		before, hp := m.Position(), m.Health()
		m.Hurt(6, entity.ProjectileDamageSource{Projectile: pl, Owner: pl})
		if m.Health() != hp {
			t.Errorf("projectile damage applied: %v -> %v", hp, m.Health())
		}
		if m.Position() != before {
			t.Errorf("enderman moved to %v with nowhere to go", m.Position())
		}
	})
}

// TestEndermanCarriedBlock: setCarryingBlock shows the block in actor data
// 23, it survives a save and reload, and it always drops on death
// (HoldBlockSystem), not as an equipment drop.
func TestEndermanCarriedBlock(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon, Persistent: true})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 4, block.Stone{}) })
	w.Mark(cube.Pos{-4, 0, -4}, cube.Pos{4, 4, 4})
	e := w.Spawn("minecraft:enderman", mgl64.Vec3{0.5, 1, 0.5})
	grass := int32(world.BlockRuntimeID(block.Grass{}))
	e.With(func(m *mob.Mob) {
		gv.SetCarried(m, block.Grass{})
		if v, _ := m.ActorData(protocol.EntityDataKeyCarryBlockRuntimeID); v != grass {
			t.Errorf("actor data 23 = %v, want grass %v", v, grass)
		}
		if main, _ := m.HeldItems(); !main.Empty() {
			t.Errorf("carried block is a held item: %v", main)
		}
		m.Goals().ClearAll() // no taking, placing or teleporting away
	})
	w.Reload()
	w.Tick(1)
	e.With(func(m *mob.Mob) {
		b, ok := gv.Carried(m)
		if _, grassOK := b.(block.Grass); !ok || !grassOK {
			t.Fatalf("after reload the enderman carries %v (%v)", b, ok)
		}
		if v, _ := m.ActorData(protocol.EntityDataKeyCarryBlockRuntimeID); v != grass {
			t.Errorf("after reload actor data 23 = %v, want grass %v", v, grass)
		}
	})
	w.Do(func(tx *world.Tx) {
		m, _ := e.Mob(tx)
		m.Hurt(1000, entity.VoidDamageSource{})
		drops := 0
		for ent := range tx.Entities() {
			if it, ok := ent.(*entity.Ent); ok {
				if b, ok := it.Behaviour().(*entity.ItemBehaviour); ok {
					if _, isGrass := b.Item().Item().(block.Grass); isGrass {
						drops += b.Item().Count()
					}
				}
			}
		}
		if drops != 1 {
			t.Errorf("%d grass dropped on death, want 1", drops)
		}
		if _, ok := gv.Carried(m); ok {
			t.Error("the dead enderman still carries its block")
		}
	})
}

// TestEndermanPlacesBlockDataCleared: placing the block sets actor data 23
// back to air.
func TestEndermanPlacesBlockDataCleared(t *testing.T) {
	w := arena(t, mobtest.Options{Time: mobtest.Noon}, 4)
	e := w.Spawn("minecraft:enderman", mgl64.Vec3{0.5, 1, 0.5})
	e.With(func(m *mob.Mob) {
		gv.SetCarried(m, block.Dirt{})
		gv.SetCarried(m, nil)
		if v, _ := m.ActorData(protocol.EntityDataKeyCarryBlockRuntimeID); v != int32(world.BlockRuntimeID(block.Air{})) {
			t.Errorf("actor data 23 after placing = %v, want air", v)
		}
	})
}
