//go:build crossfly_full

// Package mobtest is a headless test kit for definition-driven mobs: it
// builds small synchronous worlds in code, spawns mobs by identifier through
// the normal mob.Spawn path, adds a session-less player to target or tempt
// them, advances the world tick by tick and helps asserting positions and
// state. Nothing runs in the background: time only passes in Tick/TickUntil.
//
//	w := mobtest.New(t, mobtest.Options{})
//	w.Do(func(tx *world.Tx) { mobtest.Floor(tx, 0, 16, block.Stone{}) })
//	p := w.AddPlayer("target", mgl64.Vec3{8.5, 1, 0.5})
//	z := w.Spawn("minecraft:zombie", mgl64.Vec3{-8.5, 1, 0.5})
//	if _, ok := w.TickUntil(400, func(tx *world.Tx) bool { return w.Dist(z, p) < 2 }); !ok {
//		t.Fatal(w.Dump(z))
//	}
//
// The kit registers mob types itself (RegisterTypes) and never imports
// package mob/vanilla, so the mob sub-packages may use it from their own
// tests without an import cycle. Whatever mob packages the test binary links
// are what the mobs run with: tests that need behaviour import
// mob/vanilla (or the single package) and guard with Require/RequireGoal.
//
// Scenario tests for Phase 1 live in this directory's external test package
// (scenario_test.go); they skip while the packages they need are missing.
package mobtest
