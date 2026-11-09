//go:build crossfly_full

package coretest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

var once sync.Once

// InstallFilters installs a small filter compiler for tests (all_of, any_of,
// none_of and the tests is_family, has_equipment (hand), has_component,
// enum_property, rider_count, in_lava, is_baby, is_sneak_held). Package
// mob/filter must not be linked into the same test binary.
func InstallFilters() { once.Do(func() { mob.SetFilterCompiler(compile) }) }

type node struct {
	all, any, none []*node
	test           string
	subject        string
	op             string
	domain         string
	value          json.RawMessage
}

func compile(raw json.RawMessage) (mob.Filter, error) {
	n, err := parse(raw)
	if err != nil {
		return nil, err
	}
	return mob.FilterFunc(n.eval), nil
}

func parse(raw json.RawMessage) (*node, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '[' {
		var list []json.RawMessage
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, err
		}
		n := &node{}
		for _, r := range list {
			c, err := parse(r)
			if err != nil {
				return nil, err
			}
			n.all = append(n.all, c)
		}
		return n, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("filter %s: %w", raw, err)
	}
	n := &node{}
	for k, v := range obj {
		switch k {
		case "all_of", "any_of", "none_of":
			c, err := parse(v)
			if err != nil {
				return nil, err
			}
			switch k {
			case "all_of":
				n.all = append(n.all, c)
			case "any_of":
				n.any = c.all
			case "none_of":
				n.none = c.all
			}
		case "test":
			_ = json.Unmarshal(v, &n.test)
		case "subject":
			_ = json.Unmarshal(v, &n.subject)
		case "operator":
			_ = json.Unmarshal(v, &n.op)
		case "domain":
			_ = json.Unmarshal(v, &n.domain)
		case "value":
			n.value = v
		}
	}
	return n, nil
}

func (n *node) eval(ctx *mob.FilterContext) bool {
	for _, c := range n.all {
		if !c.eval(ctx) {
			return false
		}
	}
	if len(n.any) > 0 {
		ok := false
		for _, c := range n.any {
			if c.eval(ctx) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	for _, c := range n.none {
		if c.eval(ctx) {
			return false
		}
	}
	if n.test == "" {
		return true
	}
	res := n.testValue(ctx)
	if n.op == "!=" || n.op == "not" {
		return !res
	}
	return res
}

func (n *node) subjectOf(ctx *mob.FilterContext) world.Entity {
	switch n.subject {
	case "other":
		return ctx.Other
	case "target":
		return ctx.Target
	case "player":
		return ctx.Player
	case "parent":
		return ctx.Parent
	case "baby":
		return ctx.Baby
	case "damager":
		return ctx.Damager
	}
	return ctx.Self
}

func (n *node) str() string {
	var s string
	_ = json.Unmarshal(n.value, &s)
	return s
}

func (n *node) boolean() bool {
	b := true
	if len(n.value) > 0 {
		_ = json.Unmarshal(n.value, &b)
	}
	return b
}

func (n *node) testValue(ctx *mob.FilterContext) bool {
	e := n.subjectOf(ctx)
	if e == nil {
		return false
	}
	m, isMob := mob.Of(e)
	switch n.test {
	case "is_family":
		fam := n.str()
		if _, ok := e.(*player.Player); ok {
			return fam == "player"
		}
		return isMob && m.HasFamily(fam)
	case "has_equipment":
		var held item.Stack
		if h, ok := e.(interface {
			HeldItems() (item.Stack, item.Stack)
		}); ok {
			held, _ = h.HeldItems()
		}
		if held.Empty() {
			return false
		}
		want := strings.ToLower(n.str())
		data := ""
		if i := strings.LastIndexByte(want, ':'); i >= 0 {
			if _, err := strconv.Atoi(want[i+1:]); err == nil {
				want, data = want[:i], want[i+1:]
			}
		}
		want = strings.TrimPrefix(want, "minecraft:")
		name, _ := held.Item().EncodeItem()
		legacy := map[string]string{"bucket:0": "minecraft:bucket", "bucket:1": "minecraft:milk_bucket"}
		if to, ok := legacy[want+":"+data]; ok && data != "" {
			return name == to
		}
		return name == "minecraft:"+want
	case "has_component":
		return isMob && m.HasComponent(n.str())
	case "enum_property":
		if !isMob {
			return false
		}
		v, _ := m.Property(n.domain)
		return v == n.str()
	case "rider_count":
		var want float64
		_ = json.Unmarshal(n.value, &want)
		return want == 0
	case "in_lava":
		return isMob && m.Physics().InLava == n.boolean()
	case "is_baby":
		return isMob && m.Meta().Baby == n.boolean()
	case "is_sneak_held":
		p, ok := e.(*player.Player)
		return ok && p.Sneaking() == n.boolean()
	}
	return false
}
