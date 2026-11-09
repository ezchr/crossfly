//go:build crossfly_full

package filter

import (
	"encoding/json"
	"sync"
	"unsafe"
)

// rawCache holds values decoded from component JSON, keyed by the address
// of the JSON bytes. Component JSON comes from the parsed definitions and is
// never changed, so a value decoded once stays right; the length is checked
// too, as two slices may share a start. Filters run for every candidate of
// every target search, so decoding the JSON each time was most of their cost.
type rawCache[T any] struct{ m sync.Map }

type rawEntry[T any] struct {
	n  int
	v  T
	ok bool
}

// get returns parse(raw), decoding raw only the first time it is seen.
func (c *rawCache[T]) get(raw json.RawMessage, parse func(json.RawMessage) (T, bool)) (T, bool) {
	if len(raw) == 0 {
		return parse(raw)
	}
	p := unsafe.SliceData(raw)
	if v, ok := c.m.Load(p); ok {
		if e := v.(*rawEntry[T]); e.n == len(raw) {
			return e.v, e.ok
		}
		return parse(raw)
	}
	v, ok := parse(raw)
	c.m.Store(p, &rawEntry[T]{n: len(raw), v: v, ok: ok})
	return v, ok
}
