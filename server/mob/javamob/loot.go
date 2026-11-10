//go:build crossfly_full

package javamob

import (
	"embed"
	"encoding/json"
	"math"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// lootFS holds the entity loot tables of Java Edition 26.3 (data/minecraft/loot_table/entities).
//
//go:embed loot_tables
var lootFS embed.FS

// LootContext is what a loot table is rolled with (LootParams for the entity loot table set).
type LootContext struct {
	Tx *world.Tx
	// This is the mob that died.
	This *Mob
	// Killer is the entity responsible (DamageSource.getEntity), DirectKiller the one that dealt the blow (an
	// arrow), Source the damage source.
	Killer, DirectKiller world.Entity
	Source               world.DamageSource
	// ByPlayer is killed_by_player (a player hurt the mob within 100 ticks).
	ByPlayer bool
	// Looting is the looting level of the killer's weapon.
	Looting int
	Rand    *rand.Rand
}

// EntityPredicate tests an entity_properties condition of a loot table ("this", "attacker", "direct_attacker") with
// its raw predicate. The default knows the flags is_on_fire and is_baby and the vehicle's type; other mob packages
// may wrap it (SetEntityPredicate) for the predicates their tables use.
type EntityPredicate func(ctx *LootContext, entity string, pred map[string]any) bool

var entityPredicate EntityPredicate = DefaultEntityPredicate

// SetEntityPredicate replaces the entity_properties test. Call it from init only; wrap DefaultEntityPredicate.
func SetEntityPredicate(p EntityPredicate) { entityPredicate = p }

// DefaultEntityPredicate is the built-in entity_properties test.
func DefaultEntityPredicate(ctx *LootContext, ent string, pred map[string]any) bool {
	var e world.Entity
	switch ent {
	case "this":
		e = ctx.This
	case "attacker", "killer":
		e = ctx.Killer
	case "direct_attacker", "direct_killer":
		e = ctx.DirectKiller
	}
	if e == nil {
		return false
	}
	for k, v := range pred {
		switch strings.TrimPrefix(k, "minecraft:") {
		case "flags":
			flags, _ := v.(map[string]any)
			for f, want := range flags {
				w, _ := want.(bool)
				var got bool
				switch f {
				case "is_on_fire":
					if f, ok := e.(interface{ OnFireDuration() time.Duration }); ok {
						got = f.OnFireDuration() > 0
					}
				case "is_baby":
					if b, ok := e.(interface{ Baby() bool }); ok {
						got = b.Baby()
					}
				default:
					return false
				}
				if got != w {
					return false
				}
			}
		case "vehicle":
			r, ok := e.(interface{ VehicleHandle() *world.EntityHandle })
			if !ok || r.VehicleHandle() == nil {
				return false
			}
			veh, ok := r.VehicleHandle().Entity(ctx.Tx)
			if !ok {
				return false
			}
			want, _ := v.(map[string]any)
			if t, ok := want["minecraft:entity_type"].(string); ok && veh.H().Type().EncodeEntity() != t {
				return false
			}
		default:
			return false
		}
	}
	return true
}

var (
	lootMu     sync.Mutex
	lootTables = map[string]*lootTable{}
)

type lootTable struct {
	Pools []lootPool `json:"pools"`
}

type lootPool struct {
	Rolls     json.RawMessage `json:"rolls"`
	Entries   []lootEntry     `json:"entries"`
	Condition json.RawMessage `json:"condition"`
	Modifier  json.RawMessage `json:"modifier"`
}

type lootEntry struct {
	Type      string          `json:"type"`
	Name      string          `json:"name"`
	Value     string          `json:"value"`
	Weight    int             `json:"weight"`
	Condition json.RawMessage `json:"condition"`
	Modifier  json.RawMessage `json:"modifier"`
	Children  []lootEntry     `json:"children"`
}

// loadLootTable reads an embedded table ("entities/zombie").
func loadLootTable(name string) *lootTable {
	name = strings.TrimPrefix(name, "minecraft:")
	lootMu.Lock()
	defer lootMu.Unlock()
	if t, ok := lootTables[name]; ok {
		return t
	}
	var t *lootTable
	if b, err := lootFS.ReadFile("loot_tables/" + name + ".json"); err == nil {
		t = &lootTable{}
		if json.Unmarshal(b, t) != nil {
			t = nil
		}
	}
	lootTables[name] = t
	return t
}

// RollLoot rolls a loot table (LootTable.getRandomItems) and returns the stacks.
func RollLoot(name string, ctx LootContext) []item.Stack {
	t := loadLootTable(name)
	if t == nil {
		return nil
	}
	var out []item.Stack
	for _, p := range t.Pools {
		if !lootCondition(&ctx, p.Condition) {
			continue
		}
		rolls := int(numberProvider(&ctx, p.Rolls, true))
		for i := 0; i < rolls; i++ {
			for _, st := range rollPool(&ctx, p.Entries) {
				out = append(out, applyModifiers(&ctx, p.Modifier, st))
			}
		}
	}
	return out
}

func rollPool(ctx *LootContext, entries []lootEntry) []item.Stack {
	var ok []lootEntry
	total := 0
	for _, e := range entries {
		if !lootCondition(ctx, e.Condition) {
			continue
		}
		if e.Type == "minecraft:alternatives" {
			for _, c := range e.Children {
				if lootCondition(ctx, c.Condition) {
					ok = append(ok, c)
					total += max(c.Weight, 1)
					break
				}
			}
			continue
		}
		ok = append(ok, e)
		total += max(e.Weight, 1)
	}
	if total == 0 {
		return nil
	}
	pick := ctx.Rand.IntN(total)
	for _, e := range ok {
		if pick -= max(e.Weight, 1); pick >= 0 {
			continue
		}
		switch e.Type {
		case "minecraft:item":
			it, found := itemByJavaName(e.Name)
			if !found {
				return nil
			}
			st := applyModifiers(ctx, e.Modifier, item.NewStack(it, 1))
			if st.Count() <= 0 {
				return nil
			}
			return []item.Stack{st}
		case "minecraft:loot_table":
			return RollLoot(e.Value, *ctx)
		}
		return nil
	}
	return nil
}

// itemByJavaName looks a Java item name up (most mob drops have the same name on both editions).
func itemByJavaName(name string) (world.Item, bool) {
	return world.ItemByName(name, 0)
}

func decodeList(raw json.RawMessage) []map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var list []map[string]any
	if json.Unmarshal(raw, &list) == nil {
		return list
	}
	var one map[string]any
	if json.Unmarshal(raw, &one) == nil {
		return []map[string]any{one}
	}
	return nil
}

func lootCondition(ctx *LootContext, raw json.RawMessage) bool {
	for _, c := range decodeList(raw) {
		if !testCondition(ctx, c) {
			return false
		}
	}
	return true
}

func testCondition(ctx *LootContext, c map[string]any) bool {
	switch strings.TrimPrefix(str(c["type"]), "minecraft:") {
	case "killed_by_player":
		return ctx.ByPlayer
	case "random_chance":
		return ctx.Rand.Float32() < float32(num(ctx, c["chance"]))
	case "random_chance_with_enchanted_bonus":
		chance := num(ctx, c["unenchanted_chance"])
		if ctx.Looting > 0 {
			if ec, ok := c["enchanted_chance"].(map[string]any); ok && str(ec["type"]) == "minecraft:linear" {
				chance = num(ctx, ec["base"]) + num(ctx, ec["per_level_above_first"])*float64(ctx.Looting-1)
			} else {
				chance = num(ctx, c["enchanted_chance"])
			}
		}
		return ctx.Rand.Float32() < float32(chance)
	case "all_of":
		terms, _ := c["terms"].([]any)
		for _, t := range terms {
			if m, ok := t.(map[string]any); ok && !testCondition(ctx, m) {
				return false
			}
		}
		return true
	case "any_of":
		terms, _ := c["terms"].([]any)
		for _, t := range terms {
			if m, ok := t.(map[string]any); ok && testCondition(ctx, m) {
				return true
			}
		}
		return false
	case "inverted":
		t, _ := c["term"].(map[string]any)
		return !testCondition(ctx, t)
	case "entity_properties":
		p, _ := c["predicate"].(map[string]any)
		return entityPredicate(ctx, str(c["entity"]), p)
	}
	return false
}

func applyModifiers(ctx *LootContext, raw json.RawMessage, st item.Stack) item.Stack {
	for _, f := range decodeList(raw) {
		st = applyModifier(ctx, f, st)
	}
	return st
}

func applyModifier(ctx *LootContext, f map[string]any, st item.Stack) item.Stack {
	if c, ok := f["condition"]; ok {
		b, _ := json.Marshal(c)
		if !lootCondition(ctx, b) {
			return st
		}
	}
	switch strings.TrimPrefix(str(f["type"]), "minecraft:") {
	case "set_count":
		n := int(numberProviderAny(ctx, f["count"], true))
		if add, _ := f["add"].(bool); add {
			n += st.Count()
		}
		return st.Grow(n - st.Count())
	case "enchanted_count_increase":
		if ctx.Looting > 0 {
			add := int(math.Round(float64(float32(ctx.Looting) * float32(numberProviderAny(ctx, f["count"], false)))))
			st = st.Grow(add)
			if lim, ok := f["limit"].(float64); ok && lim > 0 && st.Count() > int(lim) {
				st = st.Grow(int(lim) - st.Count())
			}
		}
		return st
	case "furnace_smelt":
		if cooked, ok := smelted[strings.TrimPrefix(itemName(st), "minecraft:")]; ok {
			if it, ok := world.ItemByName("minecraft:"+cooked, 0); ok {
				return item.NewStack(it, st.Count())
			}
		}
	}
	return st
}

// smelted are the furnace results of the mob drops furnace_smelt applies to.
var smelted = map[string]string{
	"beef": "cooked_beef", "porkchop": "cooked_porkchop", "chicken": "cooked_chicken", "mutton": "cooked_mutton",
	"rabbit": "cooked_rabbit", "cod": "cooked_cod", "salmon": "cooked_salmon", "potato": "baked_potato",
}

func itemName(st item.Stack) string {
	n, _ := st.Item().EncodeItem()
	return n
}

func numberProvider(ctx *LootContext, raw json.RawMessage, integer bool) float64 {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return 1
	}
	return numberProviderAny(ctx, v, integer)
}

// numberProviderAny is a NumberProvider: a constant, or uniform (Mth.nextInt for integers, nextFloat otherwise).
func numberProviderAny(ctx *LootContext, v any, integer bool) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case map[string]any:
		switch strings.TrimPrefix(str(n["type"]), "minecraft:") {
		case "uniform":
			lo, hi := num(ctx, n["min"]), num(ctx, n["max"])
			if integer {
				a, b := int(math.Floor(lo)), int(math.Floor(hi))
				if a >= b {
					return float64(a)
				}
				return float64(a + ctx.Rand.IntN(b-a+1))
			}
			return float64(float32(lo) + ctx.Rand.Float32()*float32(hi-lo))
		case "constant":
			return num(ctx, n["value"])
		}
	}
	return 1
}

func num(ctx *LootContext, v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case map[string]any:
		return numberProviderAny(ctx, n, false)
	}
	return 0
}

func str(v any) string { s, _ := v.(string); return s }

// lootContext builds the context of the mob's death loot.
func (m *Mob) lootContext(src world.DamageSource, byPlayer bool) LootContext {
	ctx := LootContext{Tx: m.Tx(), This: m, Source: src, ByPlayer: byPlayer, Rand: m.Rand()}
	ctx.Killer = attacker(src)
	ctx.DirectKiller = ctx.Killer
	if p, ok := src.(interface{ ProjectileEntity() world.Entity }); ok {
		ctx.DirectKiller = p.ProjectileEntity()
	}
	return ctx
}
