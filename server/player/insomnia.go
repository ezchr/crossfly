package player

// TimeSinceRest returns how many ticks ago the player last slept or respawned after dying
// (BDS InsomniaComponent::mTimeSinceRest, saved as TimeSinceRest). Phantoms come to players
// whose count passed the insomnia threshold (3 days). Mob code counts it once per tick
// (InsomniaSystem); it is saved with the player.
func (p *Player) TimeSinceRest() int64 { return p.timeSinceRest }

// SetTimeSinceRest sets the ticks since the player last rested (see TimeSinceRest).
func (p *Player) SetTimeSinceRest(ticks int64) { p.timeSinceRest = ticks }
