//go:build crossfly_full

package jvillage

import (
	"bytes"
	"sort"

	"github.com/df-mc/dragonfly/server/mob/jbrain"
	"github.com/google/uuid"
)

// GossipType is Java's GossipType: weight, cap, decay per day and per
// transfer.
type GossipType uint8

const (
	MajorNegative GossipType = iota
	MinorNegative
	MinorPositive
	MajorPositive
	Trading
	gossipTypes
)

type gossipInfo struct {
	id                             string
	weight, max, decayDay, decayTr int
}

var gossipInfos = [gossipTypes]gossipInfo{
	MajorNegative: {"major_negative", -5, 100, 10, 10},
	MinorNegative: {"minor_negative", -1, 200, 20, 20},
	MinorPositive: {"minor_positive", 1, 25, 1, 5},
	MajorPositive: {"major_positive", 5, 20, 0, 20},
	Trading:       {"trading", 1, 25, 2, 20},
}

// String returns the serialised name ("major_negative").
func (t GossipType) String() string { return gossipInfos[t].id }

// Weight, Max, DecayPerDay and DecayPerTransfer are the GossipType fields.
func (t GossipType) Weight() int           { return gossipInfos[t].weight }
func (t GossipType) Max() int              { return gossipInfos[t].max }
func (t GossipType) DecayPerDay() int      { return gossipInfos[t].decayDay }
func (t GossipType) DecayPerTransfer() int { return gossipInfos[t].decayTr }

// gossipDiscard is GossipContainer.DISCARD_THRESHOLD.
const gossipDiscard = 2

// entityGossips is GossipContainer.EntityGossips: the value of each type
// (0 = absent).
type entityGossips struct {
	v   [gossipTypes]int
	has [gossipTypes]bool
}

func (e *entityGossips) empty() bool {
	for _, h := range e.has {
		if h {
			return false
		}
	}
	return true
}

func (e *entityGossips) weighted(filter func(GossipType) bool) int {
	w := 0
	for t := GossipType(0); t < gossipTypes; t++ {
		if e.has[t] && (filter == nil || filter(t)) {
			w += e.v[t] * t.Weight()
		}
	}
	return w
}

// Gossips is Java's GossipContainer: what a villager heard about entities.
// Entries are kept in UUID order (Java iterates a HashMap; the order only
// matters for which gossips a random transfer picks).
type Gossips struct {
	m map[uuid.UUID]*entityGossips
}

func (g *Gossips) getOrCreate(id uuid.UUID) *entityGossips {
	if g.m == nil {
		g.m = map[uuid.UUID]*entityGossips{}
	}
	e := g.m[id]
	if e == nil {
		e = &entityGossips{}
		g.m[id] = e
	}
	return e
}

func (g *Gossips) targets() []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(g.m))
	for id := range g.m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return bytes.Compare(ids[i][:], ids[j][:]) < 0 })
	return ids
}

// Get returns the raw value of a gossip type about id.
func (g *Gossips) Get(id uuid.UUID, t GossipType) int {
	if e := g.m[id]; e != nil && e.has[t] {
		return e.v[t]
	}
	return 0
}

// Decay is GossipContainer.decay, run once per 24000 ticks.
func (g *Gossips) Decay() {
	for id, e := range g.m {
		for t := GossipType(0); t < gossipTypes; t++ {
			if !e.has[t] {
				continue
			}
			n := e.v[t] - t.DecayPerDay()
			if n < gossipDiscard {
				e.has[t], e.v[t] = false, 0
			} else {
				e.v[t] = n
			}
		}
		if e.empty() {
			delete(g.m, id)
		}
	}
}

// Reputation is GossipContainer.getReputation: the weighted sum of the
// gossips about id that pass filter (nil: all).
func (g *Gossips) Reputation(id uuid.UUID, filter func(GossipType) bool) int {
	if e := g.m[id]; e != nil {
		return e.weighted(filter)
	}
	return 0
}

// CountForType is GossipContainer.getCountForType: how many entities have a
// weighted value of t passing test.
func (g *Gossips) CountForType(t GossipType, test func(float64) bool) int {
	n := 0
	for _, e := range g.m {
		v := 0
		if e.has[t] {
			v = e.v[t]
		}
		if test(float64(v * t.Weight())) {
			n++
		}
	}
	return n
}

// Add is GossipContainer.add.
func (g *Gossips) Add(id uuid.UUID, t GossipType, amount int) {
	e := g.getOrCreate(id)
	if e.has[t] {
		// mergeValuesForAddition
		sum := e.v[t] + amount
		if sum > t.Max() {
			e.v[t] = max(t.Max(), e.v[t])
		} else {
			e.v[t] = sum
		}
	} else {
		e.has[t], e.v[t] = true, amount
	}
	// makeSureValueIsntTooLowOrTooHigh
	if e.v[t] > t.Max() {
		e.v[t] = t.Max()
	}
	if e.v[t] < gossipDiscard {
		e.has[t], e.v[t] = false, 0
	}
	if e.empty() {
		delete(g.m, id)
	}
}

// Remove is GossipContainer.remove(target, type, amount).
func (g *Gossips) Remove(id uuid.UUID, t GossipType, amount int) { g.Add(id, t, -amount) }

// RemoveAll is GossipContainer.remove(type): drop t about everyone.
func (g *Gossips) RemoveAll(t GossipType) {
	for id, e := range g.m {
		e.has[t], e.v[t] = false, 0
		if e.empty() {
			delete(g.m, id)
		}
	}
}

// Clear is GossipContainer.clear.
func (g *Gossips) Clear() { g.m = nil }

// PutAll is GossipContainer.putAll.
func (g *Gossips) PutAll(o *Gossips) {
	for id, oe := range o.m {
		e := g.getOrCreate(id)
		for t := GossipType(0); t < gossipTypes; t++ {
			if oe.has[t] {
				e.has[t], e.v[t] = true, oe.v[t]
			}
		}
	}
}

// Copy is GossipContainer.copy.
func (g *Gossips) Copy() *Gossips {
	c := &Gossips{}
	c.PutAll(g)
	return c
}

type gossipEntry struct {
	target uuid.UUID
	t      GossipType
	v      int
}

// decompress lists the entries with a non-zero weighted value.
func (g *Gossips) decompress() []gossipEntry {
	var out []gossipEntry
	for _, id := range g.targets() {
		e := g.m[id]
		for t := GossipType(0); t < gossipTypes; t++ {
			if e.has[t] && e.v[t]*t.Weight() != 0 {
				out = append(out, gossipEntry{id, t, e.v[t]})
			}
		}
	}
	return out
}

// TransferFrom is GossipContainer.transferFrom: pick up to maxCount distinct
// gossips of src, weighted by |value*weight|, and learn them decayed by
// the type's decayPerTransfer (keeping the higher value). It returns how
// many gossips were picked.
func (g *Gossips) TransferFrom(src *Gossips, r jbrain.Random, maxCount int) int {
	entries := src.decompress()
	if len(entries) == 0 {
		return 0
	}
	ranges := make([]int, len(entries))
	end := 0
	for i, e := range entries {
		w := e.v * e.t.Weight()
		if w < 0 {
			w = -w
		}
		end += w
		ranges[i] = end - 1
	}
	picked := map[int]bool{}
	var order []int
	for range maxCount {
		choice := r.IntN(end)
		idx := sort.SearchInts(ranges, choice)
		if !picked[idx] {
			picked[idx] = true
			order = append(order, idx)
		}
	}
	for _, idx := range order {
		e := entries[idx]
		dv := e.v - e.t.DecayPerTransfer()
		if dv >= gossipDiscard {
			ge := g.getOrCreate(e.target)
			if !ge.has[e.t] || dv > ge.v[e.t] {
				ge.has[e.t], ge.v[e.t] = true, dv
			}
		}
	}
	return len(order)
}

// NBT encodes the gossips as Java's "Gossips" list:
// [{Target: [I;...], Type: "trading", Value: 5}].
func (g *Gossips) NBT() []any {
	var out []any
	for _, e := range g.decompress() {
		out = append(out, map[string]any{"Target": jbrain.UUIDCodec.Encode(e.target), "Type": e.t.String(), "Value": int32(e.v)})
	}
	return out
}

// GossipsFromNBT decodes a "Gossips" list.
func GossipsFromNBT(l []any) *Gossips {
	g := &Gossips{}
	for _, x := range l {
		c, _ := x.(map[string]any)
		id, ok := jbrain.UUIDCodec.Decode(c["Target"])
		if !ok {
			continue
		}
		name, _ := c["Type"].(string)
		v, _ := c["Value"].(int32)
		for t := GossipType(0); t < gossipTypes; t++ {
			if t.String() == name && v > 0 {
				e := g.getOrCreate(id)
				e.has[t], e.v[t] = true, int(v)
			}
		}
	}
	return g
}
