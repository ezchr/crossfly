package sound

// RespawnAnchorCharge is the sound of glowstone charging a respawn anchor.
type RespawnAnchorCharge struct{ sound }

// RespawnAnchorDeplete is the sound of a respawn anchor losing a charge when a player respawns at it.
type RespawnAnchorDeplete struct{ sound }

// RespawnAnchorSetSpawn is the sound of a respawn anchor becoming a player's spawn point.
type RespawnAnchorSetSpawn struct{ sound }

// MaceSmashAir is the sound of a mace smash attack after a short fall.
type MaceSmashAir struct{ sound }

// MaceSmashGround is the sound of a mace smash attack after a fall of up to 5 blocks.
type MaceSmashGround struct{ sound }

// MaceHeavySmashGround is the sound of a mace smash attack after a fall of more than 5 blocks.
type MaceHeavySmashGround struct{ sound }
