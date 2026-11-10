package jbrain

import "sort"

// Packed is Java's Brain.Packed: the saved memories of a brain, keyed by
// memory name.
type Packed struct {
	entries []packedEntry
}

type packedEntry struct {
	id    MemoryID
	value any
	ttl   int64
	exp   bool
}

// Pack is Brain.pack: the memories with a codec and a value.
func (m *Memories) Pack() *Packed {
	p := &Packed{}
	for _, id := range m.order {
		s := m.slots[id]
		if !s.has {
			continue
		}
		in := info(id)
		if in.encode == nil {
			continue
		}
		p.entries = append(p.entries, packedEntry{id: id, value: s.value, ttl: s.ttl, exp: s.ttl != neverExpire})
	}
	return p
}

// Pack packs the brain's memories.
func (b *Brain[E]) Pack() *Packed { return b.mem.Pack() }

// NBT encodes p the way Java writes a mob's "Brain" compound:
// {memories: {"minecraft:home": {value: ..., ttl: 123L}}}.
func (p *Packed) NBT() map[string]any {
	mems := make(map[string]any, len(p.entries))
	for _, e := range p.entries {
		in := info(e.id)
		v := map[string]any{"value": in.encode(e.value)}
		if e.exp {
			v["ttl"] = e.ttl
		}
		mems[in.name] = v
	}
	return map[string]any{"memories": mems}
}

// Unpack decodes a "Brain" compound written by NBT (or by Java). Unknown,
// unsaved or malformed memories are skipped, like Java's lenient codec.
func Unpack(brain map[string]any) *Packed {
	p := &Packed{}
	mems, _ := brain["memories"].(map[string]any)
	names := make([]string, 0, len(mems))
	for k := range mems {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, name := range names {
		id, ok := MemoryByName(name)
		if !ok {
			continue
		}
		in := info(id)
		if in.decode == nil {
			continue
		}
		c, _ := mems[name].(map[string]any)
		if c == nil {
			continue
		}
		v, ok := in.decode(c["value"])
		if !ok {
			continue
		}
		e := packedEntry{id: id, value: v, ttl: neverExpire}
		switch t := c["ttl"].(type) {
		case int64:
			e.ttl, e.exp = t, true
		case int32:
			e.ttl, e.exp = int64(t), true
		}
		p.entries = append(p.entries, e)
	}
	return p
}

// restore is the memory part of the Brain constructor: values for
// registered memories are set, others dropped.
func (p *Packed) restore(m *Memories) {
	for _, e := range p.entries {
		ttl := neverExpire
		if e.exp {
			ttl = e.ttl
		}
		m.setAny(e.id, e.value, ttl)
	}
}

// Restore sets the packed memories on b (for brains rebuilt in place, e.g.
// Villager.refreshBrain, which packs the old brain into the new one).
func (b *Brain[E]) Restore(p *Packed) { p.restore(&b.mem) }

// Codecs for the value types Java saves.

// BoolCodec is Codec.BOOL (a byte in NBT).
var BoolCodec = Codec[bool]{
	Encode: func(v bool) any {
		if v {
			return uint8(1)
		}
		return uint8(0)
	},
	Decode: func(v any) (bool, bool) {
		switch t := v.(type) {
		case uint8:
			return t != 0, true
		case int8:
			return t != 0, true
		case bool:
			return t, true
		}
		return false, false
	},
}

// LongCodec is Codec.LONG.
var LongCodec = Codec[int64]{
	Encode: func(v int64) any { return v },
	Decode: func(v any) (int64, bool) {
		switch t := v.(type) {
		case int64:
			return t, true
		case int32:
			return int64(t), true
		}
		return 0, false
	},
}

// IntCodec is Codec.INT.
var IntCodec = Codec[int]{
	Encode: func(v int) any { return int32(v) },
	Decode: func(v any) (int, bool) {
		switch t := v.(type) {
		case int32:
			return int(t), true
		case int64:
			return int(t), true
		case int16:
			return int(t), true
		case uint8:
			return int(t), true
		}
		return 0, false
	},
}

// Unit is Java's Unit value for flag memories.
type Unit struct{}

// UnitCodec is Unit.CODEC (an empty compound).
var UnitCodec = Codec[Unit]{
	Encode: func(Unit) any { return map[string]any{} },
	Decode: func(any) (Unit, bool) { return Unit{}, true },
}
