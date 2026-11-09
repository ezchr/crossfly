//go:build crossfly_full

package filter

import (
	"encoding/json"

	"github.com/df-mc/dragonfly/server/mob"
)

func init() {
	mob.SetFilterCompiler(Compile)
}

// Compile is the mob.FilterCompiler: it parses raw and returns a filter
// that evaluates against a mob.FilterContext.
func Compile(raw json.RawMessage) (mob.Filter, error) {
	f, err := Parse(raw)
	if err != nil {
		return nil, err
	}
	return mobFilter{f: f}, nil
}

type mobFilter struct{ f *Filter }

// Test ...
func (m mobFilter) Test(ctx *mob.FilterContext) bool { return m.f.Eval(FromMob(ctx)) }

// Filter returns the parsed filter tree.
func (m mobFilter) Filter() *Filter { return m.f }
