package jbrain

import (
	"fmt"
	"reflect"
	"sync"
)

// MemoryID identifies a registered memory module type (Java
// net.minecraft.world.entity.ai.memory.MemoryModuleType). IDs are dense and
// index the slots of a Memories store.
type MemoryID uint16

// Memory is a typed handle for a memory module type. Values are read and
// written with Get, Set and SetWithExpiry (Go methods cannot take type
// parameters, so these are package functions).
type Memory[T any] struct{ id MemoryID }

// ID returns the memory's registry id.
func (m Memory[T]) ID() MemoryID { return m.id }

// Name returns the memory's registry name ("minecraft:home").
func (m Memory[T]) Name() string { return MemoryName(m.id) }

// String implements fmt.Stringer.
func (m Memory[T]) String() string { return m.Name() }

// Codec converts a memory value to and from the NBT value written in a mob's
// Brain.memories compound (Java MemoryModuleType.getCodec). Encode returns a
// value nbt.Marshal accepts; Decode reports false for malformed input.
type Codec[T any] struct {
	Encode func(v T) any
	Decode func(v any) (T, bool)
}

type memoryInfo struct {
	name string
	// collection is set when the value type is a slice or map: Java stores
	// null instead of an empty collection (Brain.isEmptyCollection).
	collection bool
	encode     func(any) any
	decode     func(any) (any, bool)
}

var (
	memMu     sync.RWMutex
	memInfos  []memoryInfo
	memByName = map[string]MemoryID{}
)

// RegisterMemory registers a memory module type that is not saved with the
// mob (Java MemoryModuleType.register(name) without a codec). Register only
// from init; registering a name twice panics.
func RegisterMemory[T any](name string) Memory[T] {
	return registerMemory[T](name, nil, nil)
}

// RegisterSavedMemory registers a memory module type that is saved with the
// mob through c (Java MemoryModuleType.register(name, codec)).
func RegisterSavedMemory[T any](name string, c Codec[T]) Memory[T] {
	if c.Encode == nil || c.Decode == nil {
		panic("jbrain: RegisterSavedMemory " + name + ": incomplete codec")
	}
	enc := func(v any) any { return c.Encode(v.(T)) }
	dec := func(v any) (any, bool) {
		t, ok := c.Decode(v)
		if !ok {
			return nil, false
		}
		return t, true
	}
	return registerMemory[T](name, enc, dec)
}

func registerMemory[T any](name string, enc func(any) any, dec func(any) (any, bool)) Memory[T] {
	memMu.Lock()
	defer memMu.Unlock()
	if _, ok := memByName[name]; ok {
		panic("jbrain: memory " + name + " registered twice")
	}
	if len(memInfos) >= 1<<16-1 {
		panic("jbrain: too many memory types")
	}
	k := reflect.TypeFor[T]().Kind()
	id := MemoryID(len(memInfos))
	memInfos = append(memInfos, memoryInfo{name: name, collection: k == reflect.Slice || k == reflect.Map, encode: enc, decode: dec})
	memByName[name] = id
	return Memory[T]{id: id}
}

// MemoryName returns the registry name of id, or "" if it is unknown.
func MemoryName(id MemoryID) string {
	memMu.RLock()
	defer memMu.RUnlock()
	if int(id) < len(memInfos) {
		return memInfos[id].name
	}
	return ""
}

// MemoryByName looks up a registered memory id by name.
func MemoryByName(name string) (MemoryID, bool) {
	memMu.RLock()
	defer memMu.RUnlock()
	id, ok := memByName[name]
	return id, ok
}

func memoryCount() int {
	memMu.RLock()
	defer memMu.RUnlock()
	return len(memInfos)
}

func info(id MemoryID) memoryInfo {
	memMu.RLock()
	defer memMu.RUnlock()
	return memInfos[id]
}

// Status is a memory condition (Java MemoryStatus).
type Status uint8

const (
	// ValuePresent requires the memory to hold a value.
	ValuePresent Status = iota
	// ValueAbsent requires the memory to be empty.
	ValueAbsent
	// Registered only requires the memory to exist in the brain.
	Registered
)

// Req is one memory condition of a behaviour or activity: the pair
// (MemoryModuleType, MemoryStatus) of Java's entry conditions.
type Req struct {
	Mem    MemoryID
	Status Status
}

// Present returns the condition "m has a value".
func Present(m interface{ ID() MemoryID }) Req { return Req{Mem: m.ID(), Status: ValuePresent} }

// Absent returns the condition "m is empty".
func Absent(m interface{ ID() MemoryID }) Req { return Req{Mem: m.ID(), Status: ValueAbsent} }

// Reg returns the condition "m is registered" (BehaviorBuilder.registered):
// it never fails but registers m in the brain.
func Reg(m interface{ ID() MemoryID }) Req { return Req{Mem: m.ID(), Status: Registered} }

// neverExpire is Java's MemorySlot.NEVER_EXPIRE (Long.MAX_VALUE).
const neverExpire = int64(^uint64(0) >> 1)

// slot is Java's MemorySlot: a value and the ticks left before it expires.
type slot struct {
	registered bool
	has        bool
	value      any
	ttl        int64
}

// Memories is the memory store of a brain (the memory half of Java's
// Brain). The zero value has no registered memories.
type Memories struct {
	slots []slot
	// order lists registered ids in registration order: Pack and the
	// forgetting pass walk it.
	order []MemoryID
}

// Register makes id usable in the store (Brain.registerMemory). Values set
// for memories that are not registered are dropped, as in Java.
func (m *Memories) Register(id MemoryID) {
	if int(id) >= len(m.slots) {
		n := memoryCount()
		if n <= int(id) {
			n = int(id) + 1
		}
		s := make([]slot, n)
		copy(s, m.slots)
		m.slots = s
	}
	if !m.slots[id].registered {
		m.slots[id] = slot{registered: true, ttl: neverExpire}
		m.order = append(m.order, id)
	}
}

// IsRegistered reports whether id was registered.
func (m *Memories) IsRegistered(id MemoryID) bool {
	return int(id) < len(m.slots) && m.slots[id].registered
}

func (m *Memories) slot(id MemoryID) *slot {
	if int(id) < len(m.slots) && m.slots[id].registered {
		return &m.slots[id]
	}
	return nil
}

// Has reports whether id holds a value (Brain.hasMemoryValue).
func (m *Memories) Has(id MemoryID) bool {
	s := m.slot(id)
	return s != nil && s.has
}

// Check tests a memory condition (Brain.checkMemory). Unregistered memories
// fail every condition, Registered included.
func (m *Memories) Check(id MemoryID, st Status) bool {
	s := m.slot(id)
	if s == nil {
		return false
	}
	switch st {
	case Registered:
		return true
	case ValuePresent:
		return s.has
	default:
		return !s.has
	}
}

// CheckAll tests every condition in reqs, in order.
func (m *Memories) CheckAll(reqs []Req) bool {
	for _, r := range reqs {
		if !m.Check(r.Mem, r.Status) {
			return false
		}
	}
	return true
}

// Erase clears id (Brain.eraseMemory). Erasing an unregistered memory does
// nothing.
func (m *Memories) Erase(id MemoryID) {
	if s := m.slot(id); s != nil {
		s.has, s.value, s.ttl = false, nil, neverExpire
	}
}

// Clear erases every memory (Brain.clearMemories).
func (m *Memories) Clear() {
	for _, id := range m.order {
		m.Erase(id)
	}
}

// TimeUntilExpiry returns the ticks left before id expires; memories set
// without expiry return the maximum int64 (Brain.getTimeUntilExpiry).
func (m *Memories) TimeUntilExpiry(id MemoryID) int64 {
	if s := m.slot(id); s != nil {
		return s.ttl
	}
	return 0
}

// tick ages expiring memories by one tick and clears expired ones (Java
// Brain.forgetOutdatedMemories / MemorySlot.tick: a value set with ttl n is
// still present for n ticks and cleared on the tick after).
func (m *Memories) tick() {
	for _, id := range m.order {
		s := &m.slots[id]
		if s.has && s.ttl != neverExpire {
			if s.ttl <= 0 {
				s.has, s.value, s.ttl = false, nil, neverExpire
			} else {
				s.ttl--
			}
		}
	}
}

func (m *Memories) setAny(id MemoryID, v any, ttl int64) {
	s := m.slot(id)
	if s == nil {
		return
	}
	if v == nil || (info(id).collection && reflect.ValueOf(v).Len() == 0) {
		s.has, s.value, s.ttl = false, nil, neverExpire
		return
	}
	s.has, s.value, s.ttl = true, v, ttl
}

func (m *Memories) getAny(id MemoryID) (any, bool) {
	s := m.slot(id)
	if s == nil || !s.has {
		return nil, false
	}
	return s.value, true
}

// Get returns the value of mem and whether it has one. Reading an
// unregistered memory returns false (Java throws; Go callers check).
func Get[T any](m *Memories, mem Memory[T]) (T, bool) {
	v, ok := m.getAny(mem.id)
	if !ok {
		var zero T
		return zero, false
	}
	return v.(T), true
}

// GetOr returns the value of mem or def.
func GetOr[T any](m *Memories, mem Memory[T], def T) T {
	if v, ok := Get(m, mem); ok {
		return v
	}
	return def
}

// Set stores v in mem without expiry (Brain.setMemory). An empty slice or
// map clears the memory, like Java's empty collections; a nil pointer,
// interface, slice or map clears it too.
func Set[T any](m *Memories, mem Memory[T], v T) {
	m.setAny(mem.id, boxNil(v), neverExpire)
}

// SetWithExpiry stores v in mem for ttl ticks (Brain.setMemoryWithExpiry).
func SetWithExpiry[T any](m *Memories, mem Memory[T], v T, ttl int64) {
	m.setAny(mem.id, boxNil(v), ttl)
}

// SetOpt stores v when ok and erases mem otherwise (Brain.setMemory with an
// Optional).
func SetOpt[T any](m *Memories, mem Memory[T], v T, ok bool) {
	if ok {
		Set(m, mem, v)
	} else {
		m.Erase(mem.id)
	}
}

// Is reports whether mem holds v (Brain.isMemoryValue). T must be
// comparable at run time.
func Is[T comparable](m *Memories, mem Memory[T], v T) bool {
	cur, ok := Get(m, mem)
	return ok && cur == v
}

func boxNil[T any](v T) any {
	a := any(v)
	if a == nil {
		return nil
	}
	rv := reflect.ValueOf(a)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Map, reflect.Func, reflect.Chan:
		if rv.IsNil() {
			return nil
		}
	}
	return a
}

// String lists the memories with values, for debugging.
func (m *Memories) String() string {
	s := "{"
	first := true
	for _, id := range m.order {
		sl := m.slots[id]
		if !sl.has {
			continue
		}
		if !first {
			s += ", "
		}
		first = false
		s += fmt.Sprintf("%s=%v", MemoryName(id), sl.value)
		if sl.ttl != neverExpire {
			s += fmt.Sprintf(" (ttl %d)", sl.ttl)
		}
	}
	return s + "}"
}
