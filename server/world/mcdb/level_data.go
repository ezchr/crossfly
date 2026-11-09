package mcdb

import (
	"errors"
	"fmt"

	"github.com/df-mc/goleveldb/leveldb"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

// LevelData reads the little-endian NBT compound the database keeps under key, one of the level-wide keys of
// a Bedrock world (such as "TheEnd", where Bedrock keeps the End fight). It returns nil and no error if the key
// holds nothing.
func (db *DB) LevelData(key string) (map[string]any, error) {
	b, err := db.ldb.Get([]byte(key), nil)
	if errors.Is(err, leveldb.ErrNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("read level data %v: %w", key, err)
	}
	var m map[string]any
	if err := nbt.UnmarshalEncoding(b, &m, nbt.LittleEndian); err != nil {
		return nil, fmt.Errorf("decode level data %v: %w", key, err)
	}
	return m, nil
}

// SetLevelData stores data as the little-endian NBT compound under key (see LevelData).
func (db *DB) SetLevelData(key string, data map[string]any) error {
	b, err := nbt.MarshalEncoding(data, nbt.LittleEndian)
	if err != nil {
		return fmt.Errorf("encode level data %v: %w", key, err)
	}
	return db.ldb.Put([]byte(key), b, nil)
}
