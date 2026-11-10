//go:build crossfly_full

package mob

import (
	"bytes"
	"encoding/json"
	"strings"
)

// BDS reads some keys of a component under two names, written in its JSON
// schema as "a|b" (JsonUtil::addMember / HashedString("tame_items|tameItems")
// in the 1.21.3 *Definition::buildSchema functions): the first name found
// wins. The component factories read the snake_case name only, so before a
// component is built every other spelling is renamed to it (aliasKeys).
//
// The full list from the 1.21.3 dump, per component Crossfly implements:
var componentKeyAliases = map[string][]keyAlias{
	// TameableDefinition.
	"minecraft:tameable": aliases("tame_items|tameItems"),
	// NameableDefinition.
	"minecraft:nameable": aliases("allowNameTagRenaming|allow_name_tag_renaming", "always_show|alwaysShow"),
	// BreedableDefinition, and BreedableType for the breeds_with entries.
	"minecraft:breedable": aliases("requireTame|require_tame", "extraBabyChance|extra_baby_chance",
		"breedCooldown|breed_cooldown", "inheritTamed|inherit_tamed", "allowSitting|allow_sitting",
		"breedItems|breed_items", "breedsWith|breeds_with", "babyType|baby_type", "mateType|mate_type"),
	// MountTameableDefinition (feed_items entries: temper_mod).
	"minecraft:tamemount": aliases("minTemper|min_temper", "maxTemper|max_temper", "attemptTemperMod|attempt_temper_mod",
		"feedItems|feed_items", "autoRejectItems|auto_reject_items", "temperMod|temper_mod"),
	// AgeableDefinition.
	"minecraft:ageable": aliases("feedItems|feed_items"),
	// BoostableDefinition (boost_items entries).
	"minecraft:boostable": aliases("replaceItem|replace_item"),
	// BreathableDefinition.
	"minecraft:breathable": aliases("totalSupply|total_supply", "suffocateTime|suffocate_time", "inhaleTime|inhale_time",
		"breathesAir|breathes_air", "breathesWater|breathes_water", "breathesLava|breathes_lava",
		"breathesSolids|breathes_solids", "generatesBubbles|generates_bubbles", "breatheBlocks|breathe_blocks",
		"nonBreatheBlocks|non_breathe_blocks"),
	// ExplodeDefinition.
	"minecraft:explode": aliases("fuseLength|fuse_length", "fuseLit|fuse_lit", "causesFire|causes_fire",
		"maxResistance|max_resistance", "destroyAffectedByGriefing|destroy_affected_by_griefing",
		"fireAffectedByGriefing|fire_affected_by_griefing"),
	// DamageOverTimeDefinition.
	"minecraft:damage_over_time": aliases("damagePerHurt|damage_per_hurt", "timeBetweenHurt|time_between_hurt"),
	// ShooterDefinition.
	"minecraft:shooter": aliases("aux_val|auxVal"),
	// AngryDefinition (also renamed by package combat itself).
	"minecraft:angry": aliases("duration_delta|durationDelta", "broadcast_anger|broadcastAnger",
		"broadcast_range|broadcastRange", "broadcast_filters|broadcastFilters", "broadcast_targets|broadcastTargets",
		"calm_event|calmEvent", "angry_sound|angrySound", "sound_interval|soundInterval"),
}

// keyAlias is one "a|b" schema name: names in BDS's order, and the name the
// factories read.
type keyAlias struct {
	names     []string
	canonical string
}

// aliases parses BDS schema names; the snake_case one is the canonical name.
func aliases(specs ...string) []keyAlias {
	out := make([]keyAlias, 0, len(specs))
	for _, s := range specs {
		names := strings.Split(s, "|")
		a := keyAlias{names: names, canonical: names[0]}
		for _, n := range names {
			if strings.ToLower(n) == n {
				a.canonical = n
				break
			}
		}
		out = append(out, a)
	}
	return out
}

// aliasKeys renames the other spellings BDS accepts for key's component to
// the snake_case names, at any depth of the JSON (entries of breeds_with,
// feed_items, ...), keeping the key order. When two spellings are present
// the one BDS reads first is kept. raw is returned as it is when it has none.
func aliasKeys(key string, raw json.RawMessage) json.RawMessage {
	list, ok := componentKeyAliases[key]
	if !ok || !hasAlias(raw, list) {
		return raw
	}
	out, err := renameKeys(raw, list)
	if err != nil {
		return raw
	}
	return out
}

func hasAlias(raw json.RawMessage, list []keyAlias) bool {
	for _, a := range list {
		for _, n := range a.names {
			if n != a.canonical && bytes.Contains(raw, []byte(`"`+n+`"`)) {
				return true
			}
		}
	}
	return false
}

func renameKeys(raw json.RawMessage, list []keyAlias) (json.RawMessage, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return raw, nil
	}
	switch raw[0] {
	case '[':
		var elems []json.RawMessage
		if err := json.Unmarshal(raw, &elems); err != nil {
			return nil, err
		}
		var b bytes.Buffer
		b.WriteByte('[')
		for i, e := range elems {
			v, err := renameKeys(e, list)
			if err != nil {
				return nil, err
			}
			if i > 0 {
				b.WriteByte(',')
			}
			b.Write(v)
		}
		b.WriteByte(']')
		return b.Bytes(), nil
	case '{':
		members, err := ParseOrderedObject(raw)
		if err != nil {
			return nil, err
		}
		// For each alias, the member BDS reads: the first of its names present.
		chosen := map[int]string{} // member index -> canonical name
		skip := map[int]bool{}
		for _, a := range list {
			best, bestRank := -1, len(a.names)
			for i, m := range members {
				for r, n := range a.names {
					if m.Key == n {
						if r < bestRank {
							best, bestRank = i, r
						}
						skip[i] = true
					}
				}
			}
			if best >= 0 {
				chosen[best] = a.canonical
			}
		}
		var b bytes.Buffer
		b.WriteByte('{')
		first := true
		for i, m := range members {
			k := m.Key
			if skip[i] {
				c, ok := chosen[i]
				if !ok {
					continue
				}
				k = c
			}
			v, err := renameKeys(m.Raw, list)
			if err != nil {
				return nil, err
			}
			if !first {
				b.WriteByte(',')
			}
			first = false
			kb, _ := json.Marshal(k)
			b.Write(kb)
			b.WriteByte(':')
			b.Write(v)
		}
		b.WriteByte('}')
		return b.Bytes(), nil
	}
	return raw, nil
}
