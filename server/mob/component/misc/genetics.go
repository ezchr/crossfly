//go:build crossfly_full

package misc

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/mob/component/core"
)

// Genetics is minecraft:genetics (BDS GeneticsComponent, pandas and goats).
// Each gene has a main and a hidden allele in allele_range. A mob that is
// spawned rolls both; a baby takes one allele of each parent (a random one
// of the parent's two, or the parent's main allele with
// use_simplified_breeding), each replaced by a random allele with
// mutation_rate. On the first tick the first genetic_variants entry that
// matches fires its birth_event:
//
//	main_allele    the main allele is in range
//	hidden_allele  the hidden allele is in range
//	either_allele  one of the two is
//	both_allele    both are
//
// Alleles are saved, so loaded mobs keep their variant without an event.
type Genetics struct {
	mutation float64
	genes    []gene

	alleles map[string][2]int
	pending bool
}

type gene struct {
	name       string
	lo, hi     int
	simplified bool
	variants   []geneVariant
}

type geneVariant struct {
	main, hidden, either, both alleleMatch
	event                      sense.Trigger
}

// alleleMatch is an integer range; set false matches anything.
type alleleMatch struct {
	set    bool
	lo, hi int
}

func (a alleleMatch) has(v int) bool { return !a.set || (v >= a.lo && v <= a.hi) }

func parseAllele(raw json.RawMessage) (alleleMatch, error) {
	if isNull(raw) {
		return alleleMatch{}, nil
	}
	r, err := sense.ParseRange(raw, sense.Range{Min: -1, Max: -1})
	if err != nil {
		return alleleMatch{}, err
	}
	if r.Min < 0 && r.Max < 0 {
		return alleleMatch{}, nil
	}
	return alleleMatch{set: true, lo: int(math.Round(r.Min)), hi: int(math.Round(r.Max))}, nil
}

func newGenetics(key string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		Mutation float64 `json:"mutation_rate"`
		Genes    []struct {
			Name       string          `json:"name"`
			Range      json.RawMessage `json:"allele_range"`
			Simplified bool            `json:"use_simplified_breeding"`
			Variants   []struct {
				Main   json.RawMessage `json:"main_allele"`
				Hidden json.RawMessage `json:"hidden_allele"`
				Either json.RawMessage `json:"either_allele"`
				Both   json.RawMessage `json:"both_allele"`
				Event  json.RawMessage `json:"birth_event"`
			} `json:"genetic_variants"`
		} `json:"genes"`
	}{Mutation: 0.03125}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	c := &Genetics{mutation: v.Mutation, alleles: map[string][2]int{}}
	for _, g := range v.Genes {
		r, err := sense.ParseRange(g.Range, sense.Range{})
		if err != nil {
			return nil, fmt.Errorf("%v: allele_range: %w", key, err)
		}
		gg := gene{name: g.Name, lo: int(math.Round(r.Min)), hi: int(math.Round(r.Max)), simplified: g.Simplified}
		for _, gv := range g.Variants {
			var vv geneVariant
			for _, p := range []struct {
				dst *alleleMatch
				raw json.RawMessage
			}{{&vv.main, gv.Main}, {&vv.hidden, gv.Hidden}, {&vv.either, gv.Either}, {&vv.both, gv.Both}} {
				if *p.dst, err = parseAllele(p.raw); err != nil {
					return nil, fmt.Errorf("%v: genetic_variants: %w", key, err)
				}
			}
			if vv.event, err = sense.ParseTrigger(gv.Event); err != nil {
				return nil, fmt.Errorf("%v: birth_event: %w", key, err)
			}
			gg.variants = append(gg.variants, vv)
		}
		c.genes = append(c.genes, gg)
	}
	return c, nil
}

func (g gene) roll(m *mob.Mob) int {
	if g.hi <= g.lo {
		return g.lo
	}
	return g.lo + m.Rand().IntN(g.hi-g.lo+1)
}

// Attach rolls the alleles, inheriting them when the mob is being bred:
// core's breedable spawns the baby while both parents are still in love, so
// in-love adults of the type right at the mob are its parents.
func (c *Genetics) Attach(m *mob.Mob) {
	parents := findParents(m)
	for _, g := range c.genes {
		var a [2]int
		if len(parents) == 2 {
			for i, p := range parents {
				pa, ok := p.alleles[g.name]
				if !ok {
					a[i] = g.roll(m)
					continue
				}
				switch {
				case g.simplified:
					a[i] = pa[0]
				default:
					a[i] = pa[m.Rand().IntN(2)]
				}
				if m.Rand().Float64() < c.mutation {
					a[i] = g.roll(m)
				}
			}
		} else {
			a = [2]int{g.roll(m), g.roll(m)}
		}
		c.alleles[g.name] = a
	}
	c.pending = true
}

// findParents returns the genetics of the two in-love adults of m's type
// closest to m within 2 blocks.
func findParents(m *mob.Mob) []*Genetics {
	if m.Tx() == nil {
		return nil
	}
	p := m.Position()
	type cand struct {
		g *Genetics
		d float64
	}
	var cs []cand
	for _, e := range entitiesIn(m, p[0]-2, p[1]-2, p[2]-2, p[0]+2, p[1]+2, p[2]+2) {
		om, ok := mob.Of(e)
		if !ok || om.Identifier() != m.Identifier() || om.Baby() || !core.InLove(om) {
			continue
		}
		g, ok := mob.ComponentOf[*Genetics](om)
		if !ok {
			continue
		}
		cs = append(cs, cand{g, om.Position().Sub(p).LenSqr()})
	}
	slices.SortFunc(cs, func(a, b cand) int {
		switch {
		case a.d < b.d:
			return -1
		case a.d > b.d:
			return 1
		}
		return 0
	})
	if len(cs) < 2 {
		return nil
	}
	return []*Genetics{cs[0].g, cs[1].g}
}

// Detach ...
func (*Genetics) Detach(*mob.Mob) {}

// Alleles returns the main and hidden allele of a gene.
func (c *Genetics) Alleles(name string) (main, hidden int, ok bool) {
	a, ok := c.alleles[name]
	return a[0], a[1], ok
}

// SetAlleles changes a gene's alleles (tests, commands). The variant event
// does not fire again.
func (c *Genetics) SetAlleles(name string, main, hidden int) {
	c.alleles[name] = [2]int{main, hidden}
}

// tick fires the birth events once.
func (c *Genetics) tick(m *mob.Mob) {
	if !c.pending {
		return
	}
	c.pending = false
	for _, g := range c.genes {
		a, ok := c.alleles[g.name]
		if !ok {
			continue
		}
		for _, v := range g.variants {
			if v.main.has(a[0]) && v.hidden.has(a[1]) && (v.either.has(a[0]) || v.either.has(a[1])) && v.both.has(a[0]) && v.both.has(a[1]) {
				v.event.Fire(m, nil)
				break
			}
		}
		if cur, ok := mob.ComponentOf[*Genetics](m); !ok || cur != c {
			return
		}
	}
}

// EncodeNBT saves the alleles (BDS GeneArray: MainAllele, HiddenAllele).
func (c *Genetics) EncodeNBT() map[string]any {
	genes := map[string]any{}
	for n, a := range c.alleles {
		genes[n] = map[string]any{"MainAllele": int32(a[0]), "HiddenAllele": int32(a[1])}
	}
	return map[string]any{"Genes": genes}
}

// DecodeNBT restores them; a loaded mob fires no birth event.
func (c *Genetics) DecodeNBT(_ *mob.Mob, d map[string]any) {
	genes, ok := d["Genes"].(map[string]any)
	if !ok {
		return
	}
	for n, v := range genes {
		g, ok := v.(map[string]any)
		if !ok {
			continue
		}
		main, _ := g["MainAllele"].(int32)
		hidden, _ := g["HiddenAllele"].(int32)
		c.alleles[n] = [2]int{int(main), int(hidden)}
	}
	c.pending = false
}
