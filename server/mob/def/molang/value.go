//go:build crossfly_full

// Package molang implements the subset of Molang that vanilla entity
// definitions use outside of resource packs: set_property values, property
// defaults, experience/loot expressions and the few component fields that take
// an expression (condition, interval, success_chance, rotate_rider_by, ...).
//
// Supported: number, string ('...') and boolean literals; unary ! and -;
// binary * / + - < <= > >= == != && || ??; the ternary a ? b : c and the
// binary conditional a ? b; parentheses; statements separated by ';' with
// return; assignment to variable./temp./context. paths; query.*/q.* calls
// (resolved through Env) and the math.* library. Identifiers are case
// insensitive, so Math.Random and math.random are the same.
//
// Not supported (none of the 1.26.63 behaviour pack files use them): arrays,
// struct member access on entities (->), loop/for_each/break/continue, braces
// blocks and geometry/texture/material references.
package molang

import (
	"math"
	"strconv"
)

// Value is a Molang value: a number (booleans are 1.0 and 0.0) or a string.
type Value struct {
	Num   float64
	Str   string
	IsStr bool
}

// Num returns a number value.
func Num(f float64) Value { return Value{Num: f} }

// Bool returns 1.0 for true and 0.0 for false.
func Bool(b bool) Value {
	if b {
		return Value{Num: 1}
	}
	return Value{}
}

// Str returns a string value.
func Str(s string) Value { return Value{Str: s, IsStr: true} }

// Truthy reports whether the value counts as true: a non-zero number or a
// non-empty string.
func (v Value) Truthy() bool {
	if v.IsStr {
		return v.Str != ""
	}
	return v.Num != 0
}

// Float returns the numeric value. Strings that look like numbers convert,
// others are 0.
func (v Value) Float() float64 {
	if v.IsStr {
		f, err := strconv.ParseFloat(v.Str, 64)
		if err != nil {
			return 0
		}
		return f
	}
	return v.Num
}

// Int returns the numeric value truncated toward zero.
func (v Value) Int() int { return int(math.Trunc(v.Float())) }

// String returns the value formatted for logs and string comparisons.
func (v Value) String() string {
	if v.IsStr {
		return v.Str
	}
	return strconv.FormatFloat(v.Num, 'g', -1, 64)
}

// Equal compares two values the way Molang == does: strings compare as
// strings, everything else numerically.
func (v Value) Equal(o Value) bool {
	if v.IsStr || o.IsStr {
		return v.IsStr == o.IsStr && v.Str == o.Str
	}
	return v.Num == o.Num
}
