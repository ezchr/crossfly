//go:build crossfly_full

package boss

import (
	"encoding/json"
	"fmt"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
)

// BossBarViewer is a world.Viewer (a client session) that shows mob boss
// bars. server/session and server/java/javasession implement it
// (mob_boss.go, mobdata_boss.go): one bar per boss entity.
type BossBarViewer interface {
	// MobBossBarViewer is the player whose distance to the boss decides
	// whether the bar is shown.
	MobBossBarViewer() *world.EntityHandle
	// ShowMobBossBar shows e's bar. An empty name is the entity's own
	// (localised) name.
	ShowMobBossBar(e world.Entity, name string, health float64, colour uint8, darken bool)
	UpdateMobBossBar(e world.Entity, health float64)
	HideMobBossBar(e world.Entity)
}

// Boss bar colours (gophertunnel packet.BossEventColour*).
const (
	ColourPink          uint8 = 0
	ColourPurple        uint8 = 5
	ColourRebeccaPurple uint8 = 6
)

// Boss is minecraft:boss (BDS BossComponent/BossSystem): players within
// hud_range of the mob see a boss bar with its name and health. BDS
// re-checks the players about once a second and sends the health whenever
// it changes; the bar hides when the player leaves the range or the boss
// dies or is removed.
//
//	hud_range          55 (squared for the range test)
//	should_darken_sky  false
//	name               "" (the entity name: a name tag, else the type's
//	                   localisation key "entity.<type>.name")
//
// Colour 5 (purple); the wither sets 6 (WitherBoss::reloadHardcoded).
//
// It also carries the hard-coded parts of the two vanilla bosses that BDS
// keeps in their Actor classes (EnderDragon, WitherBoss): their damage
// rules and state live here because minecraft:boss is in both mobs' base
// components (dragon.go, wither.go).
type Boss struct {
	name     string
	hudRange float64
	darken   bool
	colour   uint8

	// health < 0 shows the mob's health; else this value (the dragon's
	// bar is 0 while it dies).
	health   float64
	shown    map[BossBarViewer]float64
	lastName string
	deadFor  int

	dragon *Dragon
	wither *Wither
}

func newBoss(_ string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		Name     string   `json:"name"`
		HudRange *float64 `json:"hud_range"`
		Darken   bool     `json:"should_darken_sky"`
	}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("minecraft:boss: %w", err)
	}
	return &Boss{name: v.Name, hudRange: or(v.HudRange, 55), darken: v.Darken, colour: ColourPurple, health: -1}, nil
}

// BossOf returns m's minecraft:boss component.
func BossOf(m *mob.Mob) (*Boss, bool) { return mob.ComponentOf[*Boss](m) }

// HudRange is hud_range.
func (b *Boss) HudRange() float64 { return b.hudRange }

// DarkensSky is should_darken_sky.
func (b *Boss) DarkensSky() bool { return b.darken }

// Colour is the bar colour.
func (b *Boss) Colour() uint8 { return b.colour }

// SetColour changes the bar colour (shown to new viewers).
func (b *Boss) SetColour(c uint8) { b.colour = c }

// SetHealthOverride shows v (0..1) on the bar instead of the health; a
// negative value shows the health again.
func (b *Boss) SetHealthOverride(v float64) { b.health = v }

// Viewers returns how many viewers see the bar.
func (b *Boss) Viewers() int { return len(b.shown) }

// Attach ...
func (b *Boss) Attach(m *mob.Mob) {
	switch m.Identifier() {
	case "minecraft:ender_dragon":
		b.dragon = DragonOf(m)
	case "minecraft:wither":
		b.colour = ColourRebeccaPurple
		b.wither = WitherOf(m)
		b.wither.attach(m)
	}
}

// Detach hides the bar.
func (b *Boss) Detach(m *mob.Mob) { b.hideAll(m) }

// OnRemove hides the bar when the boss is removed without dying (despawned,
// cleared by a command): Java keeps a bar until it is told to remove it.
func (b *Boss) OnRemove(m *mob.Mob) { b.hideAll(m) }

// TickStage runs the bar in StageSend, which dying mobs still run.
func (*Boss) TickStage() mob.Stage { return mob.StageSend }

// pct is the health share shown.
func (b *Boss) pct(m *mob.Mob) float64 {
	if b.health >= 0 {
		return min(b.health, 1)
	}
	if m.MaxHealth() <= 0 {
		return 0
	}
	return min(max(m.Health()/m.MaxHealth(), 0), 1)
}

// Tick shows, updates and hides the bar for the viewers near the mob.
func (b *Boss) Tick(m *mob.Mob) {
	if m.Dead() {
		// Health 0 is shown, then the bar goes before the mob is removed.
		if b.deadFor++; b.deadFor > 10 {
			b.hideAll(m)
			return
		}
	}
	pct := b.pct(m)
	name := m.NameTag()
	if name == "" {
		name = b.name
	}
	if name != b.lastName {
		b.hideAll(m)
		b.lastName = name
	}
	tx := m.Tx()
	if b.shown == nil || (m.Ticks()+int64(m.H().UUID()[0]))%20 == 0 {
		// BossComponent::handleRegisterPlayers, about once a second.
		near := map[BossBarViewer]bool{}
		for _, v := range tx.Viewers(m.Position()) {
			bv, ok := v.(BossBarViewer)
			if !ok {
				continue
			}
			pe, ok := bv.MobBossBarViewer().Entity(tx)
			if !ok || pe.Position().Sub(m.Position()).LenSqr() >= b.hudRange*b.hudRange {
				continue
			}
			near[bv] = true
		}
		if b.shown == nil {
			b.shown = map[BossBarViewer]float64{}
		}
		for v := range b.shown {
			if !near[v] {
				v.HideMobBossBar(m)
				delete(b.shown, v)
			}
		}
		for v := range near {
			if _, ok := b.shown[v]; !ok {
				v.ShowMobBossBar(m, name, pct, b.colour, b.darken)
				b.shown[v] = pct
			}
		}
	}
	for v, last := range b.shown {
		if last != pct {
			v.UpdateMobBossBar(m, pct)
			b.shown[v] = pct
		}
	}
}

func (b *Boss) hideAll(m *mob.Mob) {
	for v := range b.shown {
		v.HideMobBossBar(m)
	}
	clear(b.shown)
}

// ModifyDamage applies the dragon's and the wither's damage rules.
func (b *Boss) ModifyDamage(m *mob.Mob, d *mob.Damage) {
	switch {
	case b.dragon != nil:
		b.dragon.modifyDamage(m, d)
	case b.wither != nil:
		b.wither.modifyDamage(m, d)
	}
}

// OnHurt ...
func (b *Boss) OnHurt(m *mob.Mob, dealt float64, src world.DamageSource) {
	switch {
	case b.dragon != nil:
		b.dragon.onHurt(m, dealt, src)
	case b.wither != nil:
		b.wither.onHurt(m, dealt, src)
	}
}

// OnDeath hides the bar once the mob is gone (see Tick).
func (b *Boss) OnDeath(m *mob.Mob, _ world.DamageSource) {
	if b.wither != nil {
		b.wither.onDeath(m)
	}
}

// EncodeNBT saves the wither's and the dragon's hard-coded state.
func (b *Boss) EncodeNBT() map[string]any {
	switch {
	case b.wither != nil:
		return b.wither.encode()
	case b.dragon != nil:
		return b.dragon.encode()
	}
	return nil
}

// DecodeNBT ...
func (b *Boss) DecodeNBT(m *mob.Mob, data map[string]any) {
	switch {
	case b.wither != nil:
		b.wither.decode(m, data)
	case b.dragon != nil:
		b.dragon.decode(data)
	}
}
