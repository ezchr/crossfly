//go:build crossfly_full

package jvillage

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/jbrain"
	"github.com/df-mc/dragonfly/server/mob/jbrain/jmob"
	"github.com/df-mc/dragonfly/server/mob/jpoi"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type posMem = jbrain.Memory[jbrain.GlobalPos]

func sameDim(tx *world.Tx, g jbrain.GlobalPos) bool { return tx.World().Dimension() == g.Dim }

func bottomCenter(p cube.Pos) mgl64.Vec3 {
	return mgl64.Vec3{float64(p[0]) + .5, float64(p[1]), float64(p[2]) + .5}
}

// retry is AcquirePoi.JitteredLinearRetry.
type retry struct {
	prev, next int64
	delay      int
}

func newRetry(r jbrain.Random, now int64) *retry {
	t := &retry{}
	t.mark(r, now)
	return t
}

func (t *retry) mark(r jbrain.Random, now int64) {
	t.prev = now
	t.delay = min(t.delay+r.IntN(40)+40, 400)
	t.next = now + int64(t.delay)
}

// AcquirePoi is AcquirePoi.create(poiType, memoryToValidate, memoryToAcquire,
// onlyIfAdult, event, validPoi): every 20-40 ticks look for the 5 closest
// free PoIs of the type within 48 blocks, path to them and claim the one
// reached.
func AcquirePoi(pred func(*jpoi.Type) bool, validate, acquire posMem, onlyAdult bool, event *Event, valid func(tx *world.Tx, p cube.Pos) bool) jmob.Control {
	var next int64
	cache := map[cube.Pos]*retry{}
	reqs := []jbrain.Req{jbrain.Absent(acquire)}
	if validate != acquire {
		reqs = append([]jbrain.Req{jbrain.Absent(validate)}, reqs...)
	}
	return jmob.Trigger("AcquirePoi", reqs, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		if onlyAdult {
			if v, ok := Of(m); ok && v.Baby() {
				return false
			}
		}
		if next == 0 {
			next = c.Time + int64(c.Rand.IntN(20))
			return false
		}
		if c.Time < next {
			return false
		}
		next = c.Time + 20 + int64(c.Rand.IntN(20))
		// Paper stuck-entity-poi-retry-delay is off by default.
		pm := jpoi.For(c.Tx)
		for p, t := range cache {
			if c.Time-t.prev >= 400 {
				delete(cache, p)
			}
		}
		test := func(p cube.Pos) bool {
			t := cache[p]
			if t == nil {
				return true
			}
			if c.Time < t.next {
				return false
			}
			t.mark(c.Rand, c.Time)
			return true
		}
		pos := BlockPos(m)
		pm.EnsureLoadedAndValid(c.Tx, pos, 48)
		found := pm.FindNearest(c.Tx, pred, test, pos, 48, 2304, jpoi.HasSpace, 5)
		var pois []jpoi.Found
		for _, f := range found {
			if valid == nil || valid(c.Tx, f.Pos) {
				pois = append(pois, f)
			}
		}
		if target, ok := pathToPois(m, pois); ok {
			if _, ok := pm.Type(c.Tx, target); ok {
				pm.Take(c.Tx, pred, func(_ *jpoi.Type, p cube.Pos) bool { return p == target }, target, 1)
				jbrain.Set(c.Mem, acquire, jbrain.GlobalPos{Dim: c.Tx.World().Dimension(), Pos: target})
				if event != nil {
					broadcastEvent(m, *event)
				}
				clear(cache)
			}
		} else {
			for _, p := range pois {
				if cache[p.Pos] == nil {
					cache[p.Pos] = newRetry(c.Rand, c.Time)
				}
			}
		}
		return true
	})
}

// validateBedPoi is VillagerGoalPackages.validateBedPoi: a bed nobody sleeps in.
func validateBedPoi(tx *world.Tx, p cube.Pos) bool {
	b, ok := tx.Block(p).(block.Bed)
	return ok && b.Sleeper == nil
}

// ValidateNearbyPoi is ValidateNearbyPoi.create: forget a PoI within 16
// blocks whose block is gone, or a bed taken by someone else.
func ValidateNearbyPoi(pred func(*jpoi.Type) bool, mem posMem) jmob.Control {
	return jmob.Trigger("ValidateNearbyPoi", []jbrain.Req{jbrain.Present(mem)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		g, _ := jbrain.Get(c.Mem, mem)
		if !sameDim(c.Tx, g) || !jmob.CloserToCenterThan(g.Pos, m.Position(), 16) {
			return false
		}
		pm := jpoi.For(c.Tx)
		if !pm.Exists(c.Tx, g.Pos, pred) {
			c.Mem.Erase(mem.ID())
		} else if bedOccupiedByOther(c.Tx, g.Pos, m) {
			c.Mem.Erase(mem.ID())
			if !bedOccupiedByVillager(c.Tx, g.Pos) {
				pm.Release(c.Tx, g.Pos)
			}
		}
		return true
	})
}

func bedOccupiedByOther(tx *world.Tx, p cube.Pos, m *javamob.Mob) bool {
	b, ok := tx.Block(p).(block.Bed)
	if !ok || b.Sleeper == nil {
		return false
	}
	v, ok := Of(m)
	return !(ok && v.sleeping)
}

func bedOccupiedByVillager(tx *world.Tx, p cube.Pos) bool {
	for _, vm := range villagersIn(tx, cube.Box(float64(p[0]), float64(p[1]), float64(p[2]), float64(p[0]+1), float64(p[1]+1), float64(p[2]+1))) {
		if v, ok := Of(vm); ok && v.sleeping {
			return true
		}
	}
	return false
}

// PoiCompetitorScan is PoiCompetitorScan.create: of the villagers holding
// the same job site, the one with the most experience keeps it.
func PoiCompetitorScan() jmob.Control {
	return jmob.Trigger("PoiCompetitorScan", []jbrain.Req{jbrain.Present(jbrain.MemJobSite), jbrain.Present(jbrain.MemNearestLivingEntities)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		g, _ := jbrain.Get(c.Mem, jbrain.MemJobSite)
		t, ok := jpoi.For(c.Tx).Type(c.Tx, g.Pos)
		if !ok {
			return true
		}
		winner := m
		list, _ := jbrain.Get(c.Mem, jbrain.MemNearestLivingEntities)
		for _, h := range list {
			if h == m.H() {
				continue
			}
			om, ok := javamob.OfHandle(c.Tx, h)
			if !ok || om.Dead() {
				continue
			}
			ov, ok := Of(om)
			if !ok {
				continue
			}
			omem := jmob.Mem(om)
			js, ok := jbrain.Get(omem, jbrain.MemJobSite)
			if !ok || js != g || !ov.profession().HeldJobSite(t) {
				continue
			}
			winner = selectWinner(winner, om)
		}
		return true
	})
}

func selectWinner(a, b *javamob.Mob) *javamob.Mob {
	va, _ := Of(a)
	vb, _ := Of(b)
	win, lose := b, a
	if va.xp > vb.xp {
		win, lose = a, b
	}
	if mem := jmob.Mem(lose); mem != nil {
		mem.Erase(jbrain.MemJobSite.ID())
	}
	return win
}

// goToPotentialJobSite is GoToPotentialJobSite.
type goToPotentialJobSite struct {
	jmob.TaskBase
	speed float32
}

// GoToPotentialJobSite is new GoToPotentialJobSite(speed): walk to the
// claimed job site for up to 1200 ticks; release it on stop.
func GoToPotentialJobSite(speed float32) jmob.Control {
	return jmob.Behaviour("GoToPotentialJobSite", []jbrain.Req{jbrain.Present(jbrain.MemPotentialJobSite)}, 1200, 1200, &goToPotentialJobSite{speed: speed})
}

func (g *goToPotentialJobSite) CanStart(c *jbrain.Ctx, m *javamob.Mob) bool {
	b, ok := jmob.Of(m)
	if !ok {
		return true
	}
	a, ok := b.ActiveNonCoreActivity()
	return !ok || a == jbrain.ActIdle || a == jbrain.ActWork || a == jbrain.ActPlay
}

func (g *goToPotentialJobSite) CanStillUse(c *jbrain.Ctx, _ *javamob.Mob) bool {
	return c.Mem.Has(jbrain.MemPotentialJobSite.ID())
}

func (g *goToPotentialJobSite) Tick(c *jbrain.Ctx, m *javamob.Mob) {
	p, _ := jbrain.Get(c.Mem, jbrain.MemPotentialJobSite)
	jmob.SetWalkAndLookTargetMemories(c.Mem, jbrain.TrackBlock(p.Pos), g.speed, 1)
}

func (g *goToPotentialJobSite) Stop(c *jbrain.Ctx, m *javamob.Mob) {
	if p, ok := jbrain.Get(c.Mem, jbrain.MemPotentialJobSite); ok && sameDim(c.Tx, p) {
		pm := jpoi.For(c.Tx)
		if pm.Exists(c.Tx, p.Pos, jpoi.Any) {
			pm.Release(c.Tx, p.Pos)
		}
	}
	c.Mem.Erase(jbrain.MemPotentialJobSite.ID())
}

// YieldJobSite is YieldJobSite.create(speed): a jobless villager hands its
// claimed job site to a nearby villager of that profession without one.
func YieldJobSite(speed float32) jmob.Control {
	return jmob.Trigger("YieldJobSite", []jbrain.Req{jbrain.Present(jbrain.MemPotentialJobSite), jbrain.Absent(jbrain.MemJobSite), jbrain.Present(jbrain.MemNearestLivingEntities), jbrain.Reg(jbrain.MemWalkTarget), jbrain.Reg(jbrain.MemLookTarget)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		v, ok := Of(m)
		if !ok || v.Baby() || v.profession() != ProfNone {
			return false
		}
		g, _ := jbrain.Get(c.Mem, jbrain.MemPotentialJobSite)
		t, ok := jpoi.For(c.Tx).Type(c.Tx, g.Pos)
		if !ok {
			return true
		}
		list, _ := jbrain.Get(c.Mem, jbrain.MemNearestLivingEntities)
		for _, h := range list {
			om, ok := javamob.OfHandle(c.Tx, h)
			if !ok || om.H() == m.H() || om.Dead() {
				continue
			}
			ov, ok := Of(om)
			if !ok || !nearbyWantsJobsite(om, ov, t, g.Pos) {
				continue
			}
			c.Mem.Erase(jbrain.MemWalkTarget.ID())
			c.Mem.Erase(jbrain.MemLookTarget.ID())
			c.Mem.Erase(jbrain.MemPotentialJobSite.ID())
			omem := jmob.Mem(om)
			if !omem.Has(jbrain.MemJobSite.ID()) {
				jmob.SetWalkAndLookTargetMemories(omem, jbrain.TrackBlock(g.Pos), speed, 1)
				jbrain.Set(omem, jbrain.MemPotentialJobSite, jbrain.GlobalPos{Dim: c.Tx.World().Dimension(), Pos: g.Pos})
			}
			break
		}
		return true
	})
}

func nearbyWantsJobsite(om *javamob.Mob, ov *Villager, t *jpoi.Type, pos cube.Pos) bool {
	mem := jmob.Mem(om)
	if mem == nil || mem.Has(jbrain.MemPotentialJobSite.ID()) {
		return false
	}
	if !ov.profession().HeldJobSite(t) {
		return false
	}
	js, ok := jbrain.Get(mem, jbrain.MemJobSite)
	if !ok {
		p := om.Navigation().CreatePath(om, pos, t.ValidRange)
		return p != nil && p.CanReach()
	}
	return js.Pos == pos
}

// AssignProfessionFromJobSite is AssignProfessionFromJobSite.create: within
// 2 blocks of the claimed job site it becomes the job site, and a jobless
// villager takes the profession that works there.
func AssignProfessionFromJobSite() jmob.Control {
	return jmob.Trigger("AssignProfessionFromJobSite", []jbrain.Req{jbrain.Present(jbrain.MemPotentialJobSite), jbrain.Reg(jbrain.MemJobSite)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		g, _ := jbrain.Get(c.Mem, jbrain.MemPotentialJobSite)
		if !jmob.CloserToCenterThan(g.Pos, m.Position(), 2) {
			return false
		}
		c.Mem.Erase(jbrain.MemPotentialJobSite.ID())
		jbrain.Set(c.Mem, jbrain.MemJobSite, g)
		broadcastEvent(m, EventHappy)
		v, ok := Of(m)
		if !ok || v.profession() != ProfNone {
			return true
		}
		if t, ok := jpoi.For(c.Tx).Type(c.Tx, g.Pos); ok {
			for _, p := range Professions {
				if p.HeldJobSite(t) {
					v.Data.Profession = p
					v.professionChanged(m)
					v.refreshBrain(m)
					break
				}
			}
		}
		return true
	})
}

// ResetProfession is ResetProfession.create: a villager without a job site
// that never traded loses its profession.
func ResetProfession() jmob.Control {
	return jmob.Trigger("ResetProfession", []jbrain.Req{jbrain.Absent(jbrain.MemJobSite)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		v, ok := Of(m)
		if !ok {
			return false
		}
		p := v.profession()
		if p != ProfNone && p != ProfNitwit && v.xp == 0 && v.Data.Level <= 1 {
			v.Data.Profession = ProfNone
			v.professionChanged(m)
			v.refreshBrain(m)
			return true
		}
		return false
	})
}

// SetWalkTargetFromBlockMemory is SetWalkTargetFromBlockMemory.create(memory,
// speed, closeEnough, tooFar, tooLongUnreachable).
func SetWalkTargetFromBlockMemory(mem posMem, speed float32, closeEnough, tooFar, tooLong int) jmob.Control {
	return jmob.Trigger("SetWalkTargetFromBlockMemory", []jbrain.Req{jbrain.Reg(jbrain.MemCantReachWalkTargetSince), jbrain.Absent(jbrain.MemWalkTarget), jbrain.Present(mem)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		g, _ := jbrain.Get(c.Mem, mem)
		since, hasSince := jbrain.Get(c.Mem, jbrain.MemCantReachWalkTargetSince)
		v, _ := Of(m)
		giveUp := func() {
			if v != nil {
				v.releasePoi(m, mem)
			}
			c.Mem.Erase(mem.ID())
			jbrain.Set(c.Mem, jbrain.MemCantReachWalkTargetSince, c.Time)
		}
		if !sameDim(c.Tx, g) || (hasSince && c.Time-since > int64(tooLong)) {
			giveUp()
			return true
		}
		body := BlockPos(m)
		if manhattan(g.Pos, body) > tooFar {
			var towards mgl64.Vec3
			found := false
			for tries := 1; ; tries++ {
				if p, ok := jmob.DefaultRandomPosTowards(m, 15, 7, bottomCenter(g.Pos), math.Pi/2); ok {
					towards, found = p, true
					if manhattan(jbrain.Containing(p), body) <= tooFar {
						break
					}
				}
				if tries == 1000 {
					giveUp()
					return true
				}
			}
			if found {
				jbrain.Set(c.Mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: jbrain.TrackVec(towards), Speed: speed, CloseEnough: closeEnough})
			}
		} else if manhattan(g.Pos, body) > closeEnough {
			jbrain.Set(c.Mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: jbrain.TrackBlock(g.Pos), Speed: speed, CloseEnough: closeEnough})
		}
		return true
	})
}

func manhattan(a, b cube.Pos) int {
	d := func(x int) int {
		if x < 0 {
			return -x
		}
		return x
	}
	return d(a[0]-b[0]) + d(a[1]-b[1]) + d(a[2]-b[2])
}

// StrollAroundPoi is StrollAroundPoi.create(memory, speed, maxDist).
func StrollAroundPoi(mem posMem, speed float32, maxDist int) *jbrain.OneShot[*javamob.Mob] {
	var next int64
	return jmob.Trigger("StrollAroundPoi", []jbrain.Req{jbrain.Reg(jbrain.MemWalkTarget), jbrain.Present(mem)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		g, _ := jbrain.Get(c.Mem, mem)
		if !sameDim(c.Tx, g) || !jmob.CloserToCenterThan(g.Pos, m.Position(), float64(maxDist)) {
			return false
		}
		if c.Time <= next {
			return true
		}
		if p, ok := m.LandRandomPos(8, 6); ok {
			jbrain.Set(c.Mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: jbrain.TrackVec(p), Speed: speed, CloseEnough: 1})
		} else {
			c.Mem.Erase(jbrain.MemWalkTarget.ID())
		}
		next = c.Time + 180
		return true
	})
}

// StrollToPoi is StrollToPoi.create(memory, speed, closeEnough, maxDist).
func StrollToPoi(mem posMem, speed float32, closeEnough, maxDist int) *jbrain.OneShot[*javamob.Mob] {
	var next int64
	return jmob.Trigger("StrollToPoi", []jbrain.Req{jbrain.Reg(jbrain.MemWalkTarget), jbrain.Present(mem)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		g, _ := jbrain.Get(c.Mem, mem)
		if !sameDim(c.Tx, g) || !jmob.CloserToCenterThan(g.Pos, m.Position(), float64(maxDist)) {
			return false
		}
		if c.Time <= next {
			return true
		}
		jbrain.Set(c.Mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: jbrain.TrackBlock(g.Pos), Speed: speed, CloseEnough: closeEnough})
		next = c.Time + 80
		return true
	})
}

// StrollToPoiList is StrollToPoiList.create(listMemory, speed, closeEnough,
// maxDist, mustBeCloseTo).
func StrollToPoiList(list jbrain.Memory[[]jbrain.GlobalPos], speed float32, closeEnough, maxDist int, near posMem) *jbrain.OneShot[*javamob.Mob] {
	var next int64
	return jmob.Trigger("StrollToPoiList", []jbrain.Req{jbrain.Reg(jbrain.MemWalkTarget), jbrain.Present(list), jbrain.Present(near)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		l, _ := jbrain.Get(c.Mem, list)
		stay, _ := jbrain.Get(c.Mem, near)
		if len(l) == 0 {
			return false
		}
		t := l[c.Rand.IntN(len(l))]
		if !sameDim(c.Tx, t) || !jmob.CloserToCenterThan(stay.Pos, m.Position(), float64(maxDist)) {
			return false
		}
		if c.Time > next {
			jbrain.Set(c.Mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: jbrain.TrackBlock(t.Pos), Speed: speed, CloseEnough: closeEnough})
			next = c.Time + 100
		}
		return true
	})
}

// workAtPoi is WorkAtPoi (and WorkAtComposter for farmers).
type workAtPoi struct {
	jmob.TaskBase
	lastCheck int64
	composter bool
}

// WorkAtPoi is new WorkAtPoi() / new WorkAtComposter(): every 300 ticks, at
// the job site, work (sound, the farmer's composting and bread), and
// restock if due.
func WorkAtPoi(composter bool) jmob.Control {
	return jmob.Behaviour("WorkAtPoi", []jbrain.Req{jbrain.Present(jbrain.MemJobSite), jbrain.Reg(jbrain.MemLookTarget)}, jbrain.DefaultDuration, jbrain.DefaultDuration, &workAtPoi{composter: composter})
}

func (w *workAtPoi) CanStart(c *jbrain.Ctx, m *javamob.Mob) bool {
	if c.Time-w.lastCheck < 300 {
		return false
	}
	if c.Rand.IntN(2) != 0 {
		return false
	}
	w.lastCheck = c.Time
	g, _ := jbrain.Get(c.Mem, jbrain.MemJobSite)
	return sameDim(c.Tx, g) && jmob.CloserToCenterThan(g.Pos, m.Position(), 1.73)
}

func (w *workAtPoi) Start(c *jbrain.Ctx, m *javamob.Mob) {
	jbrain.Set(c.Mem, jbrain.MemLastWorkedAtPoi, c.Time)
	if g, ok := jbrain.Get(c.Mem, jbrain.MemJobSite); ok {
		jbrain.Set[jbrain.PositionTracker](c.Mem, jbrain.MemLookTarget, jbrain.TrackBlock(g.Pos))
	}
	v, ok := Of(m)
	if !ok {
		return
	}
	v.playWorkSound(m)
	if w.composter {
		v.workAtComposter(c, m)
	}
	if v.ShouldRestock(m) {
		v.Restock(m)
	}
}

func (w *workAtPoi) CanStillUse(c *jbrain.Ctx, m *javamob.Mob) bool {
	g, ok := jbrain.Get(c.Mem, jbrain.MemJobSite)
	return ok && sameDim(c.Tx, g) && jmob.CloserToCenterThan(g.Pos, m.Position(), 1.73)
}
