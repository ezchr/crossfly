//go:build crossfly_full

package animal

import (
	"encoding/binary"

	"github.com/google/uuid"
)

// The save data uses the Java names and types (TAG_Int ints, TAG_Byte booleans, UUIDs as 4-int arrays like
// Java's UUIDUtil.CODEC), read back tolerant of the integer width a decoder chose.

func boolByte(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}

// NBTInt reads an integer of any width from m[key], def if it is missing.
func NBTInt(m map[string]any, key string, def int32) int32 { return nbtInt(m, key, def) }

// NBTBool reads a byte or bool from m[key], def if it is missing.
func NBTBool(m map[string]any, key string, def bool) bool { return nbtBool(m, key, def) }

func nbtInt(m map[string]any, key string, def int32) int32 {
	switch v := m[key].(type) {
	case int32:
		return v
	case int64:
		return int32(v)
	case int:
		return int32(v)
	case int16:
		return int32(v)
	case uint8:
		return int32(v)
	case int8:
		return int32(v)
	case float32:
		return int32(v)
	case float64:
		return int32(v)
	}
	return def
}

func nbtBool(m map[string]any, key string, def bool) bool {
	switch v := m[key].(type) {
	case bool:
		return v
	case uint8:
		return v != 0
	case int8:
		return v != 0
	case int32:
		return v != 0
	}
	return def
}

// uuidInts is a UUID as Java writes it: four big-endian ints, most significant first.
func uuidInts(id uuid.UUID) []int32 {
	out := make([]int32, 4)
	for i := range out {
		out[i] = int32(binary.BigEndian.Uint32(id[i*4:]))
	}
	return out
}

// UUIDInts is uuidInts for other packages.
func UUIDInts(id uuid.UUID) []int32 { return uuidInts(id) }

// NBTUUID reads a UUID saved by UUIDInts (or as a string), uuid.Nil if it is missing.
func NBTUUID(m map[string]any, key string) uuid.UUID { return nbtUUID(m, key) }

func nbtUUID(m map[string]any, key string) uuid.UUID {
	var id uuid.UUID
	switch v := m[key].(type) {
	case []int32:
		if len(v) == 4 {
			for i, n := range v {
				binary.BigEndian.PutUint32(id[i*4:], uint32(n))
			}
		}
	case []any:
		if len(v) == 4 {
			for i, n := range v {
				x, _ := n.(int32)
				binary.BigEndian.PutUint32(id[i*4:], uint32(x))
			}
		}
	case string:
		id, _ = uuid.Parse(v)
	}
	return id
}
