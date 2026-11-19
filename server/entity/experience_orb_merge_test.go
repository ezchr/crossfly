package entity

import (
	"context"
	"log/slog"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Two orbs within OrbMergeRadius become one, in the place of the one that stays, keeping its
// motion, with both orbs' experience: no new orb with a new hop.
func TestOrbMergeKeepsOrb(t *testing.T) {
	old := OrbMergeRadius
	OrbMergeRadius = 3
	defer func() { OrbMergeRadius = old }()
	w := world.Config{Log: slog.New(slog.DiscardHandler), Synchronous: true}.New()
	defer func() { _ = w.Close() }()
	if err := w.Do(func(tx *world.Tx) {
		still := mgl64.Vec3{0, 1e-9, 0}
		a := tx.AddEntity(NewExperienceOrb(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 10, 0.5}, Velocity: still}, 3)).(*Ent)
		b := tx.AddEntity(NewExperienceOrb(world.EntitySpawnOpts{Position: mgl64.Vec3{1.5, 10, 0.5}, Velocity: still}, 7)).(*Ent)
		a.Behaviour().(*ExperienceOrbBehaviour).tick(a, tx)
		var orbs []*Ent
		for e := range tx.Entities() {
			if e.H().Type() == ExperienceOrbType {
				orbs = append(orbs, e.(*Ent))
			}
		}
		if len(orbs) != 1 {
			t.Fatalf("%d orbs after merging, want 1", len(orbs))
		}
		if orbs[0].H() != b.H() {
			t.Error("the orb that stays is a new one")
		}
		if xp := orbs[0].Behaviour().(*ExperienceOrbBehaviour).Experience(); xp != 10 {
			t.Errorf("merged orb has %d experience, want 10", xp)
		}
		if v := orbs[0].Velocity(); v.Len() > 1e-6 {
			t.Errorf("merged orb hops: velocity %v", v)
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
