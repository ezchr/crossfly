//go:build crossfly_full

package core

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// lootFS holds the loot tables the vanilla entity definitions reference
// (loot, interact spawn_items, equipment), merged from the BDS 1.26.63
// behaviour packs (newest pack wins).
//
//go:embed loot_tables
var lootFS embed.FS

var (
	lootMu     sync.Mutex
	lootTables = map[string]*LootTable{}
	extraFS    []fs.FS
)

// AddLootTables makes loot tables in fsys available (paths
// "loot_tables/..."), for custom packs. Tables found there win over the
// embedded ones. Call it before mobs load.
func AddLootTables(fsys fs.FS) {
	lootMu.Lock()
	defer lootMu.Unlock()
	extraFS = append([]fs.FS{fsys}, extraFS...)
	lootTables = map[string]*LootTable{}
}

// LoadLootTable returns the parsed loot table at path ("loot_tables/
// entities/cow.json"); tables are parsed once.
func LoadLootTable(path string) (*LootTable, error) {
	path = strings.TrimPrefix(strings.ReplaceAll(path, "\\", "/"), "/")
	lootMu.Lock()
	defer lootMu.Unlock()
	if t, ok := lootTables[path]; ok {
		return t, nil
	}
	var data []byte
	var err error
	for _, f := range extraFS {
		if data, err = fs.ReadFile(f, path); err == nil {
			break
		}
	}
	if data == nil {
		if data, err = lootFS.ReadFile(path); err != nil {
			return nil, fmt.Errorf("loot table %v: %w", path, err)
		}
	}
	t := &LootTable{Path: path}
	if err := json.Unmarshal(data, t); err != nil {
		return nil, fmt.Errorf("loot table %v: %w", path, err)
	}
	lootTables[path] = t
	return t, nil
}

var missingTables sync.Map

// warnMissingTable logs once that a referenced loot table does not exist (BDS
// logs a content error and the table drops nothing).
func warnMissingTable(path string) {
	if _, err := LoadLootTable(path); err != nil {
		if _, seen := missingTables.LoadOrStore(path, true); !seen {
			slog.Default().Warn("mob: loot table not found", "table", path, "err", err)
		}
	}
}

// LootContext is what a loot table is rolled for (BDS LootTableContext).
type LootContext struct {
	// Mob is "this" entity (the one killed, sheared, equipped, ...).
	Mob *mob.Mob
	// Killer is the entity responsible, or nil.
	Killer world.Entity
	// Killed is the entity killed when that is not Mob, such as the mob a
	// frog eats (LootTableContext::Builder::withKilledActor), or nil.
	Killed world.Entity
	// ByPlayer: killed by (or recently hit by) a player.
	ByPlayer bool
	// Looting is the highest Looting level of the killer's hand items.
	Looting int
	depth   int
}

// LootTable is a Bedrock loot table (BDS LootTable / LootPool / LootItem).
type LootTable struct {
	Path  string     `json:"-"`
	Pools []lootPool `json:"pools"`
}

type lootPool struct {
	Rolls      rangeValue      `json:"rolls"`
	BonusRolls float64         `json:"bonus_rolls"`
	Conditions []lootCondition `json:"conditions"`
	Entries    []lootEntry     `json:"entries"`
	Tiers      *struct {
		InitialRange int     `json:"initial_range"`
		BonusRolls   int     `json:"bonus_rolls"`
		BonusChance  float64 `json:"bonus_chance"`
	} `json:"tiers"`
}

type lootEntry struct {
	Type       string          `json:"type"`
	Name       string          `json:"name"`
	Weight     *int            `json:"weight"`
	Quality    int             `json:"quality"`
	Functions  []lootFunction  `json:"functions"`
	Conditions []lootCondition `json:"conditions"`
	Pools      []lootPool      `json:"pools"`
}

type lootFunction struct {
	Function   string          `json:"function"`
	Count      rangeValue      `json:"count"`
	Data       rangeValue      `json:"data"`
	Damage     rangeValue      `json:"damage"`
	Levels     rangeValue      `json:"levels"`
	Chance     float64         `json:"chance"`
	Treasure   bool            `json:"treasure"`
	ID         string          `json:"id"`
	Type       int             `json:"type"`
	Enchants   []specificEnch  `json:"enchants"`
	Conditions []lootCondition `json:"conditions"`
}

// specificEnch is one specific_enchants entry: {"id": "sharpness", "level":
// n or [min, max]} or just the id.
type specificEnch struct {
	ID    string
	Level rangeValue
}

func (s *specificEnch) UnmarshalJSON(b []byte) error {
	if json.Unmarshal(b, &s.ID) == nil {
		s.Level = rangeValue{Min: 1, Max: 1, Set: true}
		return nil
	}
	var o struct {
		ID    string     `json:"id"`
		Level rangeValue `json:"level"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return err
	}
	s.ID, s.Level = o.ID, o.Level
	if !s.Level.Set {
		s.Level = rangeValue{Min: 1, Max: 1, Set: true}
	}
	return nil
}

type lootCondition struct {
	Condition         string          `json:"condition"`
	Chance            float64         `json:"chance"`
	LootingMultiplier float64         `json:"looting_multiplier"`
	DefaultChance     *float64        `json:"default_chance"`
	Peaceful          *float64        `json:"peaceful"`
	Easy              *float64        `json:"easy"`
	Normal            *float64        `json:"normal"`
	Hard              *float64        `json:"hard"`
	MaxChance         float64         `json:"max_chance"`
	Entity            string          `json:"entity"`
	EntityType        string          `json:"entity_type"`
	Properties        map[string]bool `json:"properties"`
	Value             json.RawMessage `json:"value"`
	Domain            string          `json:"domain"`
}

// lootItem is an item being built by an entry and its functions, resolved
// to a stack at the end (legacy name:data pairs need the final data). mods
// run on the stack once it exists (enchanting, potions, banners), in
// function order.
type lootItem struct {
	name    string
	data    int16
	hasData bool
	count   int
	damage  float64 // fraction of durability lost, -1: none
	mods    []func(item.Stack) item.Stack
}

// Roll rolls every pool and returns the items.
func (t *LootTable) Roll(ctx *LootContext) []item.Stack {
	if t == nil || ctx.depth > 8 {
		return nil
	}
	var out []item.Stack
	for i := range t.Pools {
		out = append(out, t.Pools[i].roll(ctx)...)
	}
	return out
}

func (p *lootPool) roll(ctx *LootContext) []item.Stack {
	m := ctx.Mob
	if !conditionsPass(p.Conditions, ctx) {
		return nil
	}
	var out []item.Stack
	if p.Tiers != nil {
		tier := 1
		if p.Tiers.InitialRange > 1 {
			tier = 1 + m.Rand().IntN(p.Tiers.InitialRange)
		}
		for i := 0; i < p.Tiers.BonusRolls; i++ {
			if m.Rand().Float64() < p.Tiers.BonusChance {
				tier++
			}
		}
		if len(p.Entries) > 0 {
			e := &p.Entries[min(tier, len(p.Entries))-1]
			out = append(out, e.produce(ctx)...)
		}
		return out
	}
	rolls := 1
	if p.Rolls.Set {
		rolls = p.Rolls.rollInt(m)
	}
	for i := 0; i < rolls; i++ {
		if e := p.pick(ctx); e != nil {
			out = append(out, e.produce(ctx)...)
		}
	}
	return out
}

func (e *lootEntry) weight() int {
	if e.Weight == nil {
		return 1
	}
	return max(*e.Weight, 0)
}

func (p *lootPool) pick(ctx *LootContext) *lootEntry {
	total := 0
	for i := range p.Entries {
		if conditionsPass(p.Entries[i].Conditions, ctx) {
			total += p.Entries[i].weight()
		}
	}
	if total <= 0 {
		return nil
	}
	n := ctx.Mob.Rand().IntN(total)
	for i := range p.Entries {
		e := &p.Entries[i]
		if !conditionsPass(e.Conditions, ctx) {
			continue
		}
		if n -= e.weight(); n < 0 {
			return e
		}
	}
	return nil
}

func (e *lootEntry) produce(ctx *LootContext) []item.Stack {
	switch e.Type {
	case "loot_table":
		t, err := LoadLootTable(e.Name)
		if err != nil {
			return nil
		}
		c := *ctx
		c.depth++
		return t.Roll(&c)
	case "item":
		li := &lootItem{count: 1, damage: -1}
		d := ParseItemDescriptor(e.Name)
		li.name, li.data, li.hasData = d.Name, d.Data, d.HasData
		for _, f := range e.Functions {
			f.apply(li, ctx)
		}
		if li.count <= 0 {
			return nil
		}
		s, ok := ItemDescriptor{Name: li.name, Data: li.data, HasData: li.hasData}.Stack(li.count)
		if !ok {
			return nil
		}
		if li.damage >= 0 {
			if dur, ok := s.Item().(item.Durable); ok {
				s = s.WithDurability(max(1, int(math.Round(float64(dur.DurabilityInfo().MaxDurability)*(1-li.damage)))))
			}
		}
		for _, mod := range li.mods {
			s = mod(s)
		}
		if mc := s.MaxCount(); s.Count() > mc {
			var out []item.Stack
			for n := s.Count(); n > 0; n -= mc {
				out = append(out, s.Grow(min(n, mc)-s.Count()))
			}
			return out
		}
		return []item.Stack{s}
	}
	return nil
}

func (f lootFunction) apply(li *lootItem, ctx *LootContext) {
	if !conditionsPass(f.Conditions, ctx) {
		return
	}
	m := ctx.Mob
	r := m.Rand()
	switch strings.TrimPrefix(f.Function, "minecraft:") {
	case "set_count":
		if f.Count.Set {
			li.count = f.Count.rollInt(m)
		}
	case "looting_enchant":
		// BDS LootingEnchantFunction: count += round(rand[min, max] x
		// looting) when a mob (or player) killed with Looting.
		if ctx.Looting > 0 && f.Count.Set {
			li.count += int(math.Round(f.Count.roll(m) * float64(ctx.Looting)))
		}
	case "set_data":
		if f.Data.Set {
			li.data, li.hasData = int16(f.Data.rollInt(m)), true
		}
	case "set_data_from_color_index":
		li.data, li.hasData = int16(m.Meta().Color), true
	case "set_damage":
		if f.Damage.Set {
			li.damage = 1 - f.Damage.roll(m)
		}
	case "furnace_smelt":
		s, ok := ItemDescriptor{Name: li.name, Data: li.data, HasData: li.hasData}.Stack(1)
		if !ok {
			return
		}
		if sm, ok := s.Item().(item.Smeltable); ok {
			if p := sm.SmeltInfo().Product; !p.Empty() {
				name, meta := p.Item().EncodeItem()
				li.name, li.data, li.hasData = name, meta, meta != 0
			}
		}
	case "enchant_random_gear":
		// BDS EnchantRandomEquipmentFunction.
		mult := loot.SpecialMultiplier(m.Tx())
		chance := f.Chance
		li.mods = append(li.mods, func(s item.Stack) item.Stack { return loot.EnchantRandomGear(r, s, chance, mult) })
	case "enchant_randomly":
		// BDS EnchantRandomlyFunction: randomlyEnchant at level rand % 30.
		treasure := f.Treasure
		li.mods = append(li.mods, func(s item.Stack) item.Stack {
			return loot.RandomlyEnchant(r, s, int(r.Uint32()%30), treasure)
		})
	case "enchant_with_levels":
		// BDS EnchantWithLevelsFunction.
		lv, treasure := f.Levels, f.Treasure
		li.mods = append(li.mods, func(s item.Stack) item.Stack {
			return loot.RandomlyEnchant(r, s, lv.rollInt(m), treasure)
		})
	case "specific_enchants":
		var es []item.Enchantment
		for _, e := range f.Enchants {
			if t, ok := enchantmentByName(e.ID); ok {
				es = append(es, item.NewEnchantment(t, max(e.Level.rollInt(m), 1)))
			}
		}
		li.mods = append(li.mods, func(s item.Stack) item.Stack { return s.WithEnchantments(es...) })
	case "set_potion":
		if p, ok := potionByName(f.ID); ok {
			li.mods = append(li.mods, func(s item.Stack) item.Stack { return withPotion(s, p) })
		}
	case "set_banner_details":
		if f.Type == 1 {
			li.mods = append(li.mods, func(s item.Stack) item.Stack {
				if _, ok := s.Item().(block.Banner); !ok {
					return s
				}
				return item.NewStack(OminousBanner(), s.Count())
			})
		}
	}
	// set_ominous_bottle_amplifier, set_lore, set_name, ...: not modelled.
}

// OminousBanner is the illager captain banner (BDS set_banner_details type 1,
// BannerBlockActor ominous patterns).
func OminousBanner() block.Banner {
	l := func(t block.BannerPatternType, c item.Colour) block.BannerPatternLayer {
		return block.BannerPatternLayer{Type: t, Colour: c}
	}
	return block.Banner{Colour: item.ColourWhite(), Illager: true, Patterns: []block.BannerPatternLayer{
		l(block.RhombusBannerPattern(), item.ColourCyan()),
		l(block.StripeBottomBannerPattern(), item.ColourLightGrey()),
		l(block.StripeCentreBannerPattern(), item.ColourGrey()),
		l(block.BorderBannerPattern(), item.ColourLightGrey()),
		l(block.StripeMiddleBannerPattern(), item.ColourBlack()),
		l(block.HalfHorizontalBannerPattern(), item.ColourLightGrey()),
		l(block.CircleBannerPattern(), item.ColourLightGrey()),
		l(block.BorderBannerPattern(), item.ColourBlack()),
	}}
}

func enchantmentByName(id string) (item.EnchantmentType, bool) {
	id = strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(id, "minecraft:")), "_", "")
	for _, t := range item.Enchantments() {
		if strings.ReplaceAll(strings.ToLower(t.Name()), " ", "") == id {
			return t, true
		}
	}
	return nil, false
}

// potionByName maps set_potion ids to potions.
func potionByName(id string) (potion.Potion, bool) {
	id = strings.TrimPrefix(strings.ToLower(id), "minecraft:")
	m := map[string]func() potion.Potion{
		"water": potion.Water, "mundane": potion.Mundane, "thick": potion.Thick, "awkward": potion.Awkward,
		"night_vision": potion.NightVision, "invisibility": potion.Invisibility, "leaping": potion.Leaping,
		"fire_resistance": potion.FireResistance, "swiftness": potion.Swiftness, "slowness": potion.Slowness,
		"water_breathing": potion.WaterBreathing, "healing": potion.Healing, "harming": potion.Harming,
		"poison": potion.Poison, "regeneration": potion.Regeneration, "strength": potion.Strength,
		"weakness": potion.Weakness, "wither": potion.Wither, "turtle_master": potion.TurtleMaster,
		"slow_falling": potion.SlowFalling, "long_poison": potion.LongPoison, "long_slowness": potion.LongSlowness,
		"long_weakness": potion.LongWeakness, "strong_poison": potion.StrongPoison,
	}
	if f, ok := m[id]; ok {
		return f(), true
	}
	return potion.Potion{}, false
}

func withPotion(s item.Stack, p potion.Potion) item.Stack {
	switch s.Item().(type) {
	case item.Arrow:
		return item.NewStack(item.Arrow{Tip: p}, s.Count())
	case item.Potion:
		return item.NewStack(item.Potion{Type: p}, s.Count())
	case item.SplashPotion:
		return item.NewStack(item.SplashPotion{Type: p}, s.Count())
	case item.LingeringPotion:
		return item.NewStack(item.LingeringPotion{Type: p}, s.Count())
	}
	return s
}

func conditionsPass(cs []lootCondition, ctx *LootContext) bool {
	for _, c := range cs {
		if !c.test(ctx) {
			return false
		}
	}
	return true
}

func (c lootCondition) test(ctx *LootContext) bool {
	m := ctx.Mob
	r := m.Rand()
	switch strings.TrimPrefix(c.Condition, "minecraft:") {
	case "killed_by_player":
		return ctx.ByPlayer
	case "killed_by_player_or_pets":
		return ctx.ByPlayer || isPlayerOrPet(m, ctx.Killer)
	case "random_chance":
		return r.Float64() < c.Chance
	case "random_chance_with_looting":
		return r.Float64() < c.Chance+float64(ctx.Looting)*c.LootingMultiplier
	case "random_difficulty_chance":
		ch := 0.0
		if c.DefaultChance != nil {
			ch = *c.DefaultChance
		}
		var d *float64
		switch m.Tx().World().Difficulty() {
		case world.DifficultyPeaceful:
			d = c.Peaceful
		case world.DifficultyEasy:
			d = c.Easy
		case world.DifficultyNormal:
			d = c.Normal
		case world.DifficultyHard:
			d = c.Hard
		}
		if d != nil {
			ch = *d
		}
		return r.Float64() < ch
	case "random_regional_difficulty_chance":
		// BDS LootItemRandomChanceWithSpecialModifierCondition: max_chance x
		// the special multiplier (clamped regional difficulty).
		return float32(c.MaxChance*loot.SpecialMultiplier(m.Tx())) > r.Float32()
	case "entity_properties":
		for k, v := range c.Properties {
			switch k {
			case "on_fire":
				if (m.OnFireDuration() > 0) != v {
					return false
				}
			}
		}
		return true
	case "killed_by_entity", "damaged_by_entity":
		if ctx.Killer == nil {
			return false
		}
		return identifierOf(ctx.Killer) == normaliseID(c.EntityType)
	case "entity_killed":
		if ctx.Killed != nil {
			return identifierOf(ctx.Killed) == normaliseID(c.EntityType)
		}
		return m.Identifier() == normaliseID(c.EntityType)
	case "has_variant":
		var v int32
		_ = json.Unmarshal(c.Value, &v)
		return m.Meta().Variant == v
	case "has_mark_variant":
		var v int32
		_ = json.Unmarshal(c.Value, &v)
		return m.Meta().MarkVariant == v
	case "is_baby":
		return IsBaby(m)
	case "bool_property":
		v, ok := m.Property(c.Domain)
		want := true
		if len(c.Value) > 0 {
			_ = json.Unmarshal(c.Value, &want)
		}
		b, _ := v.(bool)
		return ok && b == want
	case "passenger_of_entity":
		return false
	}
	return false
}

func normaliseID(id string) string {
	if id != "" && !strings.Contains(id, ":") {
		return "minecraft:" + id
	}
	return id
}

func identifierOf(e world.Entity) string {
	if m, ok := mob.Of(e); ok {
		return m.Identifier()
	}
	return e.H().Type().EncodeEntity()
}

// DropItems drops stacks at the mob as item entities.
func DropItems(m *mob.Mob, stacks []item.Stack, yOffset float64) {
	tx := m.Tx()
	pos := m.Position().Add(mgl64.Vec3{0, yOffset, 0})
	for _, s := range stacks {
		if s.Empty() {
			continue
		}
		r := m.Rand()
		vel := mgl64.Vec3{r.Float64()*0.2 - 0.1, 0.2, r.Float64()*0.2 - 0.1}
		tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: pos, Velocity: vel}, s))
	}
}

// deathContext returns the loot context of a mob killed by src.
func deathContext(m *mob.Mob, src world.DamageSource) *LootContext {
	ctx := &LootContext{Mob: m, ByPlayer: LastHitByPlayer(m), Killer: sourceEntity(src)}
	if ctx.Killer == nil {
		if e, _, ok := m.LastHurtBy(); ok {
			ctx.Killer = e
		}
	}
	ctx.Looting = loot.HandLooting(ctx.Killer)
	return ctx
}

// loot is minecraft:loot {"table"}: dropped when the mob dies (BDS
// LootComponent, Actor::dropDeathLoot).
type lootComponent struct{ table string }

func newLoot(key string, raw json.RawMessage) (mob.Component, error) {
	var j struct {
		Table string `json:"table"`
	}
	if err := decode(raw, &j); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	if j.Table != "" {
		warnMissingTable(j.Table)
	}
	return &lootComponent{table: j.Table}, nil
}

func (*lootComponent) Attach(*mob.Mob) {}
func (*lootComponent) Detach(*mob.Mob) {}

// OnDeath drops the loot.
func (c *lootComponent) OnDeath(m *mob.Mob, src world.DamageSource) {
	if c.table == "" {
		return
	}
	t, err := LoadLootTable(c.table)
	if err != nil {
		return
	}
	DropItems(m, t.Roll(deathContext(m, src)), 0)
}

// headDrops are the heads mobs drop when a charged creeper kills them (BDS
// Mob::die: skull data by actor type). ActorClassTree::isInstanceOf with a
// full actor type compares the type byte exactly, so strays, husks,
// drowned, zombie villagers and piglin brutes drop nothing.
var headDrops = map[string]int16{
	"minecraft:skeleton":        0,
	"minecraft:wither_skeleton": 1,
	"minecraft:zombie":          2,
	"minecraft:creeper":         4,
	"minecraft:piglin":          6,
}

func init() {
	mob.RegisterDeathHook(mob.DeathHook{Name: "core/charged_creeper_head", Die: dropChargedCreeperHead})
}

// dropChargedCreeperHead drops the mob's head, 1 block up, when a charged
// creeper's explosion killed it.
func dropChargedCreeperHead(m *mob.Mob, src world.DamageSource) {
	es, ok := src.(entity.ExplosionDamageSource)
	if !ok {
		return
	}
	ees, ok := es.Source.(world.EntityExplosionSource)
	if !ok {
		return
	}
	c, ok := ees.Entity.(*mob.Mob)
	if !ok || c.Identifier() != "minecraft:creeper" || !c.Meta().Charged {
		return
	}
	data, ok := headDrops[m.Identifier()]
	if !ok {
		return
	}
	if s, ok := (ItemDescriptor{Name: "minecraft:skull", Data: data, HasData: true}).Stack(1); ok {
		DropItems(m, []item.Stack{s}, 1)
	}
}

// experienceReward is minecraft:experience_reward {"on_death", "on_bred"}:
// Molang expressions (or lists of them, summed) for the experience dropped
// on death and given for breeding (BDS ExperienceRewardComponent; each
// expression is rounded). The death experience drops when the corpse is
// removed, 20 ticks after death (Mob::tickDeath), evaluated then (the
// equipment that dropped on death no longer counts in equipment_count).
type experienceReward struct {
	onDeath, onBred []Expr
	diedAt          int64 // tick of death + 1, 0 while alive
	dropped         bool
}

func parseExprList(raw json.RawMessage) ([]Expr, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) != nil {
		list = []json.RawMessage{raw}
	}
	out := make([]Expr, 0, len(list))
	for _, r := range list {
		e, err := ParseExpr(r)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func newExperienceReward(key string, raw json.RawMessage) (mob.Component, error) {
	var j struct {
		OnDeath json.RawMessage `json:"on_death"`
		OnBred  json.RawMessage `json:"on_bred"`
	}
	if err := decode(raw, &j); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	c := &experienceReward{}
	var err error
	if c.onDeath, err = parseExprList(j.OnDeath); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	if c.onBred, err = parseExprList(j.OnBred); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	return c, nil
}

func (*experienceReward) Attach(*mob.Mob) {}
func (*experienceReward) Detach(*mob.Mob) {}

func sumExprs(m *mob.Mob, es []Expr) int {
	n := 0
	for _, e := range es {
		n += int(math.Round(e.Eval(m)))
	}
	return n
}

// experienceDelay is how long after death the experience drops (Mob::
// tickDeath drops it with the corpse at deathTime 20).
const experienceDelay = 19

// OnDeath starts the countdown to the death experience.
func (c *experienceReward) OnDeath(m *mob.Mob, _ world.DamageSource) {
	c.diedAt = m.Ticks() + 1
}

// TickStage ...
func (*experienceReward) TickStage() mob.Stage { return mob.StageDespawn }

// Tick drops the death experience once the corpse is about to go.
func (c *experienceReward) Tick(m *mob.Mob) {
	if c.diedAt == 0 || c.dropped || m.Ticks()+1-c.diedAt < experienceDelay {
		return
	}
	c.dropped = true
	DropExperience(m, sumExprs(m, c.onDeath))
}

// BredExperience returns the experience m gives for breeding (on_bred).
func BredExperience(m *mob.Mob) int {
	c, ok := mob.ComponentOf[*experienceReward](m)
	if !ok {
		return 0
	}
	return sumExprs(m, c.onBred)
}

// DeathExperience returns what m would drop dying now (on_death).
func DeathExperience(m *mob.Mob) int {
	c, ok := mob.ComponentOf[*experienceReward](m)
	if !ok {
		return 0
	}
	return sumExprs(m, c.onDeath)
}

// DropExperience drops amount experience as orbs at the mob.
func DropExperience(m *mob.Mob, amount int) {
	if amount <= 0 {
		return
	}
	for _, h := range entity.NewExperienceOrbs(m.Position(), amount) {
		m.Tx().AddEntity(h)
	}
}
