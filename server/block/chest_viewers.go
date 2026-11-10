package block

// Viewers is the number of players that have the chest open
// (ChestBlockEntity.getOpenCount): cats only sit on closed chests.
func (c Chest) Viewers() int { return len(c.viewers) }
