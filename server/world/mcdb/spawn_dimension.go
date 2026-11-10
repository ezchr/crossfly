package mcdb

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

// LoadPlayerSpawnDimension loads the dimension of a player's spawn point: SpawnDimension in the player's server
// data, where BDS keeps it too. exists is false if the player has no server data or no SpawnDimension in it.
func (db *DB) LoadPlayerSpawnDimension(id uuid.UUID) (dim int, exists bool, err error) {
	serverData, _, exists, err := db.loadPlayerData(id)
	if !exists || err != nil {
		return 0, false, err
	}
	v, ok := serverData["SpawnDimension"].(int32)
	if !ok {
		return 0, false, nil
	}
	return int(v), true, nil
}

// SavePlayerSpawnDimension saves the dimension of a player's spawn point as SpawnDimension in the player's server
// data. It does nothing for a player without server data: SavePlayerSpawnPosition creates it.
func (db *DB) SavePlayerSpawnDimension(id uuid.UUID, dim int) error {
	d, k, exists, err := db.loadPlayerData(id)
	if err != nil {
		return err
	}
	if !exists || d == nil {
		return nil
	}
	d["SpawnDimension"] = int32(dim)
	data, err := nbt.MarshalEncoding(d, nbt.LittleEndian)
	if err != nil {
		return fmt.Errorf("encode server data for player %v: %w", id, err)
	}
	if err = db.ldb.Put([]byte(k), data, nil); err != nil {
		return fmt.Errorf("write server data for player %v: %w", id, err)
	}
	return nil
}
