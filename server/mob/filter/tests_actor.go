//go:build crossfly_full

package filter

import (
	"math"
	"strings"
)

// Actor tests: state of the subject itself. Semantics follow the BDS 1.21.3
// Actor*Test classes unless a test notes another source.

func init() {
	// Boolean state tests: raw = the flag, compared with the value (default
	// true) by the operator.
	for name, f := range map[string]Flag{
		"in_water":                   FlagInWater,
		"in_lava":                    FlagInLava,
		"is_underwater":              FlagUnderwater,
		"in_water_or_rain":           FlagInWaterOrRain,
		"in_contact_with_water":      FlagInContactWithWater,
		"on_ground":                  FlagOnGround,
		"on_fire":                    FlagOnFire,
		"on_hot_block":               FlagOnHotBlock,
		"taking_fire_damage":         FlagTakingFireDamage,
		"is_baby":                    FlagBaby,
		"is_riding":                  FlagRiding,
		"is_sneaking":                FlagSneaking,
		"is_sneak_held":              FlagSneakHeld,
		"is_sprinting":               FlagSprinting,
		"is_sleeping":                FlagSleeping,
		"is_sitting":                 FlagSitting,
		"is_moving":                  FlagMoving,
		"is_climbing":                FlagClimbing,
		"on_ladder":                  FlagOnLadder,
		"is_immobile":                FlagImmobile,
		"is_tamed":                   FlagTamed,
		"is_leashed":                 FlagLeashed,
		"is_persistent":              FlagPersistent,
		"is_panicking":               FlagPanicking,
		"is_avoiding_mobs":           FlagAvoidingMobs,
		"is_raider":                  FlagRaider,
		"in_caravan":                 FlagInCaravan,
		"is_visible":                 FlagVisible,
		"is_navigating":              FlagNavigating,
		"has_container_open":         FlagContainerOpen,
		"has_nametag":                FlagNameTag,
		"has_ranged_weapon":          FlagRangedWeapon,
		"has_trade_supply":           FlagTradeSupply,
		"is_in_village":              FlagInVillage,
		"is_bound_to_creaking_heart": FlagBoundToCreakingHeart,
		"surface_mob":                FlagSurfaceMob,
		"in_clouds":                  FlagInClouds,
	} {
		f := f
		Register(&Def{Name: name, Kind: KindBool, Source: "bds 1.21.3 / docs",
			Eval: func(in *Input, s *Scope) bool { return in.CmpBool(s.Actor.Flag(f)) }})
	}

	// String membership tests: raw = has(value), compared with true.
	Register(&Def{Name: "is_family", Kind: KindString, Source: "bds 1.21.3 ActorIsFamilyTest",
		Eval: func(in *Input, s *Scope) bool { return in.CmpTrue(s.Actor.HasFamily(in.Str)) }})
	Register(&Def{Name: "has_component", Kind: KindString, Source: "bds 1.21.3 ActorHasComponentTest",
		Eval: func(in *Input, s *Scope) bool { return in.CmpTrue(s.Actor.HasComponent(in.Str)) }})
	Register(&Def{Name: "has_tag", Kind: KindString, Source: "bds 1.21.3 ActorHasTagTest",
		Eval: func(in *Input, s *Scope) bool {
			t, ok := s.Actor.(Tagged)
			return in.CmpTrue(ok && t.HasTag(in.Str))
		}})
	Register(&Def{Name: "has_ability", Kind: KindString, Source: "bds 1.21.3 ActorHasAbilityTest",
		Eval: func(in *Input, s *Scope) bool {
			a, ok := s.Actor.(Abilities)
			return in.CmpTrue(ok && a.HasAbility(in.Str))
		}})
	Register(&Def{Name: "has_mob_effect", Kind: KindString, Source: "bds 1.21.3 ActorHasMobEffect",
		Eval: func(in *Input, s *Scope) bool {
			e, ok := s.Actor.(Effects)
			return in.CmpTrue(ok && e.HasEffect(stripNS(in.Str)))
		}})
	Register(&Def{Name: "is_color", Kind: KindString, Source: "bds 1.21.3 ActorIsColorTest",
		Eval: func(in *Input, s *Scope) bool {
			c, ok := s.Actor.(Coloured)
			return in.CmpTrue(ok && sameColour(c.Colour(), in.Str))
		}})

	// Numeric tests.
	Register(&Def{Name: "actor_health", Kind: KindInt, Source: "bds 1.21.3 ActorHealthTest (Actor::getHealth is an int)",
		Eval: func(in *Input, s *Scope) bool {
			h, _ := number(s.Actor, NumberHealth)
			return in.CmpInt(trunc(h))
		}})
	Register(&Def{Name: "is_missing_health", Kind: KindBool, Source: "bds 1.21.3 ActorMissingHealthTest",
		Eval: func(in *Input, s *Scope) bool {
			h, ok1 := number(s.Actor, NumberHealth)
			m, ok2 := number(s.Actor, NumberMaxHealth)
			return in.CmpBool(ok1 && ok2 && h < m)
		}})
	for name, n := range map[string]Number{
		"rider_count":     NumberRiderCount,
		"is_variant":      NumberVariant,
		"is_mark_variant": NumberMarkVariant,
		"is_skin_id":      NumberSkinID,
	} {
		n := n
		Register(&Def{Name: name, Kind: KindInt, Source: "bds 1.21.3",
			Eval: func(in *Input, s *Scope) bool {
				v, _ := number(s.Actor, n)
				return in.CmpInt(trunc(v))
			}})
	}
	Register(&Def{Name: "y_rotation", Kind: KindFloat, Source: "docs (1.21.50+): body yaw in [-180, 180)",
		Eval: func(in *Input, s *Scope) bool {
			v, _ := number(s.Actor, NumberYRotation)
			return in.CmpFloat(wrapDegrees(v))
		}})
	Register(&Def{Name: "home_distance", Kind: KindFloat, Source: "docs",
		Eval: func(in *Input, s *Scope) bool {
			v, ok := number(s.Actor, NumberHomeDistance)
			return ok && in.CmpFloat(v)
		}})
	Register(&Def{Name: "inactivity_timer", Kind: KindInt, Source: "bds 1.21.3 ActorInactivityTimerTest: timer reached",
		Eval: func(in *Input, s *Scope) bool {
			v, _ := number(s.Actor, NumberInactivitySeconds)
			return cmpBool(in.Op, v >= float64(in.Int), true)
		}})
	Register(&Def{Name: "random_chance", Kind: KindInt, Source: "bds 1.21.3 ActorRandomChanceTest: nextInt(value)==0, operator ignored",
		Eval: func(in *Input, s *Scope) bool { return s.Ctx.intN(in.Int) == 0 }})

	// Relations.
	Register(&Def{Name: "has_target", Kind: KindBool, Source: "bds 1.21.3 ActorHasTargetTest",
		Eval: func(in *Input, s *Scope) bool { return in.CmpBool(related(s.Actor, RelationTarget) != nil) }})
	Register(&Def{Name: "is_target", Kind: KindBool, Source: "bds 1.21.3 ActorIsTargetTest: subject is the calling entity's target",
		Eval: func(in *Input, s *Scope) bool {
			t := related(s.Ctx.Self, RelationTarget)
			return in.CmpBool(same(t, s.Actor))
		}})
	Register(&Def{Name: "is_owner", Kind: KindBool, Source: "bds 1.21.3 ActorIsOwnerTest: subject owns the calling entity",
		Eval: func(in *Input, s *Scope) bool {
			o := related(s.Ctx.Self, RelationOwner)
			return in.CmpBool(same(o, s.Actor))
		}})
	Register(&Def{Name: "was_last_hurt_by", Kind: KindBool, Source: "bds 1.21.3 ActorWasLastHurtByTest",
		Eval: func(in *Input, s *Scope) bool {
			h := related(s.Ctx.Self, RelationLastHurtBy)
			return in.CmpBool(same(h, s.Actor))
		}})
	Register(&Def{Name: "is_leashed_to", Kind: KindBool, Source: "bds 1.21.3 ActorIsLeashedToTest: subject leashed to the calling entity",
		Eval: func(in *Input, s *Scope) bool {
			h := related(s.Actor, RelationLeashHolder)
			return in.CmpBool(same(h, s.Ctx.Self))
		}})
	Register(&Def{Name: "is_riding_self", Kind: KindBool, Source: "docs: subject rides the calling entity",
		Eval: func(in *Input, s *Scope) bool {
			v := related(s.Actor, RelationVehicle)
			return in.CmpBool(same(v, s.Ctx.Self))
		}})
	Register(&Def{Name: "trusts", Kind: KindBool, Source: "bds 1.21.3 ActorTrustsSubjectTest: calling entity trusts subject",
		Eval: func(in *Input, s *Scope) bool {
			t, ok := s.Ctx.Self.(Trusting)
			return in.CmpBool(ok && t.Trusts(s.Actor))
		}})
	Register(&Def{Name: "is_vehicle_family", Kind: KindString, Source: "docs",
		Eval: func(in *Input, s *Scope) bool {
			v := related(s.Actor, RelationVehicle)
			return in.CmpTrue(v != nil && v.HasFamily(in.Str))
		}})
	Register(&Def{Name: "is_controlling_passenger_family", Kind: KindString, Source: "docs",
		Eval: func(in *Input, s *Scope) bool {
			p := related(s.Actor, RelationControllingPassenger)
			return in.CmpTrue(p != nil && p.HasFamily(in.Str))
		}})
	Register(&Def{Name: "target_distance", Kind: KindFloat, Source: "bds 1.21.3: distance from subject to its target; false without target",
		Eval: func(in *Input, s *Scope) bool {
			t := related(s.Actor, RelationTarget)
			return t != nil && in.CmpFloat(s.Actor.Position().Sub(t.Position()).Len())
		}})
	Register(&Def{Name: "owner_distance", Kind: KindFloat, Source: "docs: false without owner",
		Eval: func(in *Input, s *Scope) bool {
			o := related(s.Actor, RelationOwner)
			return o != nil && in.CmpFloat(s.Actor.Position().Sub(o.Position()).Len())
		}})

	// Properties (description.properties). A missing property reads as the
	// zero value of its type, like PropertyComponent::tryGet* failing.
	Register(&Def{Name: "int_property", Kind: KindInt, Source: "bds 1.21.3 ActorIntPropertyTest",
		Eval: func(in *Input, s *Scope) bool {
			v, _ := property(s.Actor, in.Domain)
			n, _ := numeric(v)
			return in.CmpInt(trunc(n))
		}})
	Register(&Def{Name: "float_property", Kind: KindFloat, Source: "bds 1.21.3 ActorFloatPropertyTest",
		Eval: func(in *Input, s *Scope) bool {
			v, _ := property(s.Actor, in.Domain)
			f, _ := numeric(v)
			return in.CmpFloat(f)
		}})
	Register(&Def{Name: "bool_property", Kind: KindBool, Source: "bds 1.21.3 ActorBoolPropertyTest",
		Eval: func(in *Input, s *Scope) bool {
			v, _ := property(s.Actor, in.Domain)
			b, _ := v.(bool)
			return in.CmpBool(b)
		}})
	Register(&Def{Name: "enum_property", Kind: KindString, Source: "bds 1.21.3 ActorEnumPropertyTest (== and != only)",
		Eval: func(in *Input, s *Scope) bool {
			v, _ := property(s.Actor, in.Domain)
			str, _ := v.(string)
			return in.CmpString(str)
		}})
	Register(&Def{Name: "has_property", Kind: KindString, Source: "bds 1.21.3 ActorHasPropertyTest",
		Eval: func(in *Input, s *Scope) bool {
			_, ok := property(s.Actor, in.Str)
			return in.CmpTrue(ok)
		}})
}

func property(a Actor, name string) (any, bool) {
	p, ok := a.(PropertyHolder)
	if !ok {
		return nil, false
	}
	return p.PropertyValue(name)
}

// numeric converts a property value (int, int32, float32, float64) to float64.
func numeric(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

func wrapDegrees(d float64) float64 {
	d = math.Mod(d+180, 360)
	if d < 0 {
		d += 360
	}
	return d - 180
}

var colourAliases = map[string]string{"light_gray": "silver", "lightgray": "silver", "lightblue": "light_blue"}

func sameColour(a, b string) bool {
	a, b = strings.ToLower(stripNS(a)), strings.ToLower(stripNS(b))
	if x, ok := colourAliases[a]; ok {
		a = x
	}
	if x, ok := colourAliases[b]; ok {
		b = x
	}
	return a == b
}
