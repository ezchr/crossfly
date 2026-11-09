//go:build crossfly_full

package village

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	ccore "github.com/df-mc/dragonfly/server/mob/component/core"
	vc "github.com/df-mc/dragonfly/server/mob/component/village"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// work is minecraft:behavior.work (BDS WorkGoal): walk to the owned job
// site, look at it, play its sound every sound_delay ticks, stamp the
// village work time every 1200 ticks and fire on_arrival once; stop after
// active_time, cool down goal_cooldown ticks. Rain stops it unless
// can_work_in_rain (or within work_in_rain_tolerance of the end).
type work struct {
	mob.GoalBase
	moveToPOI
	soundMin, soundMax int
	cooldownMax        int
	activeTime         int
	rain               bool
	rainTolerance      int
	onArrival          ccore.Trigger
	composter          *composterCfg

	cooldown   int
	active     int
	soundTimer int
	soundDelay int
	arrived    bool
	nextUse    int64
	uses       int
}

type composterCfg struct {
	useMin, useMax int
	itemsMax       int
	interactions   int
	fill, empty    bool
	minItems       int
}

func newWork(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		SoundMin      int           `json:"sound_delay_min"`
		SoundMax      int           `json:"sound_delay_max"`
		Cooldown      int           `json:"goal_cooldown"`
		Active        int           `json:"active_time"`
		Rain          bool          `json:"can_work_in_rain"`
		RainTolerance *int          `json:"work_in_rain_tolerance"`
		OnArrival     ccore.Trigger `json:"on_arrival"`
		Speed         *float64      `json:"speed_multiplier"`
		UseMin        *int          `json:"use_block_min"`
		UseMax        *int          `json:"use_block_max"`
		ItemsMax      *int          `json:"items_per_use_max"`
		Interactions  *int          `json:"block_interaction_max"`
		Fill          *bool         `json:"can_fill_composter"`
		Empty         *bool         `json:"can_empty_composter"`
		MinItems      *int          `json:"min_item_count"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	g := &work{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, soundMin: j.SoundMin, soundMax: j.SoundMax,
		cooldownMax: j.Cooldown, activeTime: max(0, j.Active), rain: j.Rain, rainTolerance: -1, onArrival: j.OnArrival}
	g.typ, g.speed = vc.POIJob, 0.5
	if j.Speed != nil {
		g.speed = *j.Speed
	}
	if j.RainTolerance != nil {
		g.rainTolerance = *j.RainTolerance
	}
	if spec.Key == "minecraft:behavior.work_composter" {
		c := &composterCfg{useMin: 100, useMax: 200, itemsMax: 20, interactions: 1, fill: true, empty: true, minItems: 10}
		if j.UseMin != nil {
			c.useMin = *j.UseMin
		}
		if j.UseMax != nil {
			c.useMax = *j.UseMax
		}
		if j.ItemsMax != nil {
			c.itemsMax = *j.ItemsMax
			if c.itemsMax < 0 {
				c.itemsMax = math.MaxInt32
			}
		}
		if j.Interactions != nil {
			c.interactions = *j.Interactions
		}
		if j.Fill != nil {
			c.fill = *j.Fill
		}
		if j.Empty != nil {
			c.empty = *j.Empty
		}
		if j.MinItems != nil {
			c.minItems = *j.MinItems
		}
		g.composter = c
	}
	return g, nil
}

// insideOrDry is WorkGoal::_isInsideOrIsNotRaining.
func (g *work) insideOrDry(m *mob.Mob) bool {
	p := g.poi.Pos
	return !(raining(m.Tx(), p) && canSeeSky(m.Tx(), p.Side(cube.FaceUp)))
}

func (g *work) CanUse(m *mob.Mob) bool {
	if g.cooldown > 0 {
		if g.cooldown--; g.cooldown > 0 {
			return false
		}
	}
	if !hasNav(m) || !g.find(m) {
		return false
	}
	return g.rain || g.insideOrDry(m)
}

func (g *work) CanContinueToUse(m *mob.Mob) bool {
	if !g.valid() {
		return false
	}
	if g.activeTime > 0 && g.active > g.activeTime {
		return false
	}
	if g.rain {
		return true
	}
	if g.rainTolerance > -1 && g.activeTime-g.active <= g.rainTolerance {
		return true
	}
	return g.insideOrDry(m)
}

func (g *work) Start(m *mob.Mob) {
	g.start(m)
	g.active, g.soundTimer, g.uses, g.nextUse = 0, 0, 0, 0
	g.rollSound(m)
}

func (g *work) rollSound(m *mob.Mob) {
	g.soundDelay = g.soundMin
	if g.soundMax > g.soundMin {
		g.soundDelay += m.Rand().IntN(g.soundMax - g.soundMin)
	}
}

func (g *work) Tick(m *mob.Mob) {
	lookAtPos(m, g.poi.Pos.Vec3().Add(mgl64.Vec3{0.5, 1.5, 0.5}), 30, 30)
	if !g.tick(m, 1.5) {
		return
	}
	if g.composter != nil {
		g.useComposter(m)
	} else if g.soundTimer <= g.soundDelay {
		g.soundTimer++
	} else {
		g.rollSound(m)
		g.soundTimer = 1
		playPOISound(m, g.poi)
	}
	if g.active%1200 == 0 {
		if v, ok := vc.VillageOf(m); ok {
			v.SetWorkTimestamp(m.H())
		}
	}
	g.active++
	if !g.arrived {
		g.arrived = true
		g.onArrival.Fire(m, nil)
	}
}

func (g *work) Stop(m *mob.Mob) {
	g.stop(m)
	g.cooldown = g.cooldownMax
}

// useComposter is WorkComposterGoal::useWorkstation.
func (g *work) useComposter(m *mob.Mob) {
	c := g.composter
	now := m.Ticks()
	if g.nextUse == 0 {
		g.nextUse = now + int64(c.useMin+m.Rand().IntN(max(1, c.useMax-c.useMin)))
	}
	if g.uses >= c.interactions || now < g.nextUse {
		return
	}
	emptied := c.empty && emptyComposter(m, g.poi.Pos)
	filled := c.fill && fillComposter(m, g.poi.Pos, c)
	if !emptied && !filled {
		playPOISound(m, g.poi)
	}
	g.uses++
	g.nextUse = now + int64(c.useMin+m.Rand().IntN(max(1, c.useMax-c.useMin)))
}

func emptyComposter(m *mob.Mob, pos cube.Pos) bool {
	c, ok := m.Tx().Block(pos).(block.Composter)
	if !ok || c.Level < 8 {
		return false
	}
	c.Level = 0
	m.Tx().SetBlock(pos, c, nil)
	vc.ThrowItems(m, []item.Stack{item.NewStack(item.BoneMeal{}, 1)})
	return true
}

// fillComposter is WorkComposterGoal::_tryCompostItems: seeds beyond
// min_item_count go in, items_per_use_max at most.
func fillComposter(m *mob.Mob, pos cube.Pos, c *composterCfg) bool {
	cb, ok := m.Tx().Block(pos).(block.Composter)
	if !ok || cb.Level >= 7 || c.itemsMax == 0 {
		return false
	}
	inv := vc.Inventory(m)
	slots := inv.Slots()
	for i := len(slots) - 1; i >= 0; i-- {
		s := slots[i]
		if s.Empty() || !isSeed(s) {
			continue
		}
		d := ccore.ParseItemDescriptor(nameOf(s))
		excess := vc.CountItems(m, d) - c.minItems
		if excess <= 0 {
			continue
		}
		n := min(excess, s.Count(), c.itemsMax)
		for range n {
			if cb.Level >= 7 {
				break
			}
			if m.Rand().Float64() < 0.3 {
				cb.Level++
			}
		}
		_ = inv.SetItem(i, s.Grow(-n))
		m.Tx().SetBlock(pos, cb, nil)
		return true
	}
	return false
}

func nameOf(s item.Stack) string {
	n, _ := s.Item().EncodeItem()
	return n
}

// isSeed is Item::isSeed.
func isSeed(s item.Stack) bool {
	switch nameOf(s) {
	case "minecraft:wheat_seeds", "minecraft:beetroot_seeds", "minecraft:pumpkin_seeds", "minecraft:melon_seeds",
		"minecraft:torchflower_seeds", "minecraft:pitcher_pod":
		return true
	}
	return false
}

func playPOISound(m *mob.Mob, p *vc.POI) {
	if p == nil || p.Sound == "" {
		return
	}
	m.Tx().PlaySound(p.Pos.Vec3Centre(), mob.Sound{Event: p.Sound})
}

// sleep is minecraft:behavior.sleep (BDS SleepGoal): walk to the owned bed
// and lie in it (sleeping flag, a small collision box, locked to the bed's
// position and rotation) until the goal stops; a sleep that was not
// interrupted heals fully. timeout_cooldown is the MoveToPOI timeout.
type sleep struct {
	mob.GoalBase
	moveToPOI
	height, width, yOffset float64
	riding                 bool
	cooldown               int
	wokeUp                 bool
	wake                   int
	sleeping               bool
	pos                    mgl64.Vec3
	yaw                    float64
	interrupted            bool
	oldW, oldH             float64
}

func newSleep(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Speed    *float64 `json:"speed_multiplier"`
		Height   float64  `json:"sleep_collider_height"`
		Width    float64  `json:"sleep_collider_width"`
		Riding   bool     `json:"can_sleep_while_riding"`
		YOffset  float64  `json:"sleep_y_offset"`
		Cooldown float64  `json:"cooldown_time"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	g := &sleep{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook | mob.FlagJump)}, height: j.Height, width: j.Width,
		riding: j.Riding, yOffset: j.YOffset, cooldown: int(j.Cooldown)}
	g.typ, g.speed = vc.POIBed, 1
	if j.Speed != nil {
		g.speed = *j.Speed
	}
	return g, nil
}

func (g *sleep) CanUse(m *mob.Mob) bool {
	if !hasNav(m) || (!g.riding && m.Riding()) || !g.find(m) {
		return false
	}
	if g.wokeUp {
		if g.wake++; g.wake <= g.cooldown {
			return false
		}
		g.wokeUp, g.wake = false, 0
	}
	bed, ok := m.Tx().Block(g.poi.Pos).(block.Bed)
	if !ok {
		return false
	}
	g.setPos(bed)
	return true
}

// setPos is BedHelper::setBedPositionOffsets: the head block centre moved
// half a block towards the foot, at the bed's height plus sleep_y_offset.
func (g *sleep) setPos(bed block.Bed) {
	foot := cube.Pos{}.Side(bed.Facing.Opposite().Face())
	c := g.poi.Pos.Vec3Middle()
	g.pos = mgl64.Vec3{c[0] + float64(foot[0])*0.375, float64(g.poi.Pos[1]) + g.yOffset, c[2] + float64(foot[2])*0.375}
	g.yaw = yawOf(bed.Facing.Opposite())
}

func (g *sleep) Start(m *mob.Mob) {
	g.start(m)
	g.interrupted, g.sleeping = false, false
}

func (g *sleep) CanContinueToUse(m *mob.Mob) bool {
	if !g.valid() {
		return false
	}
	if g.reached {
		_, ok := m.Tx().Block(g.poi.Pos).(block.Bed)
		return ok
	}
	return !m.Navigation().Done() || g.repath < 70
}

func (g *sleep) Tick(m *mob.Mob) {
	if !g.sleeping && g.tick(m, 1.5) {
		g.sleeping = true
		meta := m.Meta()
		g.oldW, g.oldH = meta.Width, meta.Height
		vc.SetSleeping(m, true, g.poi.Pos)
		if g.width > 0 && g.height > 0 {
			m.UpdateMeta(func(meta *mob.Meta) { meta.Width, meta.Height = g.width, g.height })
		}
		m.StopMovement()
	}
	if g.sleeping {
		m.SetPosition(g.pos)
		m.SetVelocity(mgl64.Vec3{})
		m.SetYaw(g.yaw)
		m.SetBodyYaw(g.yaw)
		m.SetHeadYaw(g.yaw)
	}
}

func (g *sleep) Stop(m *mob.Mob) {
	if g.sleeping {
		if !g.interrupted {
			if _, t, ok := m.LastHurtBy(); !ok || m.Ticks()-t > 10 {
				m.Heal(m.MaxHealth()-m.Health(), healSleep{})
			}
		}
		wake := g.poi.Pos.Side(cube.FaceUp)
		if bed, ok := m.Tx().Block(g.poi.Pos).(block.Bed); ok {
			wake = g.poi.Pos.Side(bed.Facing.Opposite().Face())
			if vc.Solid(m.Tx(), wake) {
				wake = g.poi.Pos.Side(cube.FaceUp)
			}
		}
		m.UpdateMeta(func(meta *mob.Meta) { meta.Width, meta.Height = g.oldW, g.oldH })
		m.SetPosition(wake.Vec3Middle())
		m.SetVelocity(mgl64.Vec3{})
		vc.SetSleeping(m, false, cube.Pos{})
		g.wokeUp = true
	}
	g.sleeping = false
	g.stop(m)
}

type healSleep struct{}

func (healSleep) HealingSource() {}

// mingle is minecraft:behavior.mingle (BDS MingleGoal): go to the bell,
// find a partner of mingle_partner_type that is searching too, walk up to
// it and chat (haggle sounds) for duration seconds.
type mingle struct {
	mob.GoalBase
	moveToPOI
	duration, cooldownT int
	partnerType         string
	distance            float64

	counter   int
	state     int
	left      int
	search    int
	nextChat  int
	partner   *world.EntityHandle
	lastPartn *world.EntityHandle
}

type mingleState struct {
	state   int
	partner *world.EntityHandle
	prev    *world.EntityHandle
}

type mingleKey struct{}

func mingleOf(m *mob.Mob) *mingleState {
	if v, ok := m.Data(mingleKey{}); ok {
		return v.(*mingleState)
	}
	s := &mingleState{}
	m.SetData(mingleKey{}, s)
	return s
}

func newMingle(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Speed    *float64 `json:"speed_multiplier"`
		Duration *float64 `json:"duration"`
		Cooldown float64  `json:"cooldown_time"`
		Partner  string   `json:"mingle_partner_type"`
		Distance *float64 `json:"mingle_distance"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	g := &mingle{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, duration: 20, cooldownT: int(j.Cooldown * 20),
		partnerType: j.Partner, distance: 2}
	if j.Duration != nil {
		g.duration = int(*j.Duration * 20)
	}
	if j.Distance != nil {
		g.distance = *j.Distance
	}
	g.typ, g.speed, g.fallback = vc.POIMeeting, 1, true
	if j.Speed != nil {
		g.speed = *j.Speed
	}
	return g, nil
}

func (g *mingle) CanUse(m *mob.Mob) bool {
	if g.counter++; g.counter < g.cooldownT {
		return false
	}
	return hasNav(m) && g.find(m)
}

func (g *mingle) Start(m *mob.Mob) {
	g.start(m)
	s := mingleOf(m)
	s.state, g.left, g.search, g.nextChat = 0, g.duration, g.cooldownT, 50+m.Rand().IntN(50)
	if g.search <= 0 {
		g.search = 200
	}
}

func (g *mingle) partnerOf(m *mob.Mob) (*mob.Mob, *mingleState, bool) {
	s := mingleOf(m)
	if s.partner == nil {
		return nil, nil, false
	}
	p, ok := mob.OfHandle(m.Tx(), s.partner)
	if !ok || p.Dead() {
		return nil, nil, false
	}
	return p, mingleOf(p), true
}

func (g *mingle) CanContinueToUse(m *mob.Mob) bool {
	if g.left <= 0 || !g.valid() {
		return false
	}
	s := mingleOf(m)
	p, ps, ok := g.partnerOf(m)
	switch s.state {
	case 0:
		return true
	case 1:
		return true
	case 2:
		return ok && ps.partner == m.H() && ps.state == 3
	case 3:
		return ok && ps.partner == m.H() && ps.state == 2
	case 4:
		return ok && ps.state == 4 && ps.partner == m.H() && distSq(p.Position(), m.Position()) <= g.distance*g.distance*4
	}
	return false
}

func (g *mingle) Tick(m *mob.Mob) {
	s := mingleOf(m)
	switch s.state {
	case 0:
		if g.tick(m, g.poi.Radius*0.5+1.5) {
			s.state = 1
		}
	case 1:
		if g.search > 0 {
			g.search--
			if g.search%10 == 0 {
				g.findPartner(m, s)
			}
		} else {
			s.state = 0
			g.reached = false
		}
	case 2:
		p, ps, ok := g.partnerOf(m)
		if !ok {
			return
		}
		if distSq(p.Position(), m.Position()) <= g.distance*g.distance {
			s.state, ps.state = 4, 4
			m.Navigation().Stop(m)
		} else if m.Navigation().Done() {
			m.Navigation().MoveToEntity(m, p, g.speed)
		}
	case 3:
		m.Navigation().Stop(m)
		if p, _, ok := g.partnerOf(m); ok {
			lookAt(m, p, 30, 30)
		}
	case 4:
		g.left--
		if p, _, ok := g.partnerOf(m); ok {
			lookAt(m, p, 30, 30)
		}
		if g.nextChat > 0 && g.left%g.nextChat == 0 {
			m.PlaySound("haggle.yes")
			g.nextChat = 50 + m.Rand().IntN(50)
		}
	}
}

func (g *mingle) findPartner(m *mob.Mob, s *mingleState) {
	if g.poi == nil {
		return
	}
	c := g.poi.Centre()
	r := g.poi.Radius
	for e := range m.Tx().EntitiesWithin(cube.Box(c[0]-r, c[1]-r, c[2]-r, c[0]+r, c[1]+r, c[2]+r)) {
		o, ok := mob.Of(e)
		if !ok || o.H() == m.H() || (g.partnerType != "" && o.Identifier() != g.partnerType) || baby(o) {
			continue
		}
		os := mingleOf(o)
		if os.state != 1 || os.prev == m.H() || s.prev == o.H() {
			continue
		}
		if m.Navigation().MoveToEntity(m, o, g.speed) {
			os.prev, os.partner, os.state = os.partner, m.H(), 3
			s.prev, s.partner, s.state = s.partner, o.H(), 2
			return
		}
	}
}

func (g *mingle) Stop(m *mob.Mob) {
	s := mingleOf(m)
	if _, ps, ok := g.partnerOf(m); ok && ps.partner == m.H() {
		ps.state, ps.partner = 0, nil
	}
	if s.state == 1 {
		s.prev = nil
	}
	s.state, s.partner = 0, nil
	g.counter = 0
	g.stop(m)
}

var _ = world.Overworld

// yawOf is the yaw (degrees) of facing d.
func yawOf(d cube.Direction) float64 {
	switch d {
	case cube.South:
		return 0
	case cube.West:
		return 90
	case cube.North:
		return 180
	}
	return 270
}
