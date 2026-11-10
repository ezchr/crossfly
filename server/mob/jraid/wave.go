package jraid

import "math/rand/v2"

// Kind is an entity type a raid spawns, in the order of Raid.RaiderType (the order waves are spawned in).
type Kind int

const (
	Vindicator Kind = iota
	Evoker
	Pillager
	Witch
	Ravager
	// NoKind is no raider: a ravager without a rider.
	NoKind Kind = -1
)

// Kinds are the raider types of a wave in the order Raid.spawnGroup spawns them (Raid.RaiderType.VALUES).
var Kinds = [...]Kind{Vindicator, Evoker, Pillager, Witch, Ravager}

// ID is the Java entity identifier of the kind.
func (k Kind) ID() string {
	switch k {
	case Vindicator:
		return "minecraft:vindicator"
	case Evoker:
		return "minecraft:evoker"
	case Pillager:
		return "minecraft:pillager"
	case Witch:
		return "minecraft:witch"
	case Ravager:
		return "minecraft:ravager"
	}
	return ""
}

func (k Kind) String() string { return k.ID() }

// CanBeLeader is PatrollingMonster.canBeLeader: witches and ravagers (Witch/Ravager.canBeLeader) never carry the
// banner.
func (k Kind) CanBeLeader() bool { return k != Witch && k != Ravager && k != NoKind }

// spawnsPerWaveBeforeBonus is Raid.RaiderType.spawnsPerWaveBeforeBonus, indexed by kind then wave (index 0 unused).
var spawnsPerWaveBeforeBonus = [5][8]int{
	Vindicator: {0, 0, 2, 0, 1, 4, 2, 5},
	Evoker:     {0, 0, 0, 0, 0, 1, 1, 2},
	Pillager:   {0, 4, 3, 3, 4, 4, 4, 2},
	Witch:      {0, 0, 0, 0, 3, 0, 0, 1},
	Ravager:    {0, 0, 0, 1, 0, 1, 0, 2},
}

// NumGroups is Raid.getNumGroups: the waves of a raid before its bonus wave.
func NumGroups(d Difficulty) int {
	switch d {
	case Easy:
		return 3
	case Normal:
		return 5
	case Hard:
		return 7
	}
	return 0
}

// MaxRaidOmenLevel is Raid.getMaxRaidOmenLevel (DEFAULT_MAX_RAID_OMEN_LEVEL).
const MaxRaidOmenLevel = 5

// EnchantOdds is Raid.getEnchantOdds: the chance a pillager or vindicator of the raid gets an enchanted weapon
// (Pillager/Vindicator.applyRaidBuffs).
func EnchantOdds(raidOmenLevel int) float32 {
	switch raidOmenLevel {
	case 2:
		return 0.1
	case 3:
		return 0.25
	case 4:
		return 0.5
	case 5:
		return 0.75
	}
	return 0
}

// defaultNumSpawns is Raid.getDefaultNumSpawns.
func defaultNumSpawns(k Kind, wave, numGroups int, bonus bool) int {
	if bonus {
		return spawnsPerWaveBeforeBonus[k][numGroups]
	}
	return spawnsPerWaveBeforeBonus[k][wave]
}

// potentialBonusSpawns is Raid.getPotentialBonusSpawns: the random extra raiders of a kind, by the difficulty of
// the regional difficulty at the spawn position.
func potentialBonusSpawns(k Kind, r *rand.Rand, wave int, d Difficulty, bonus bool) int {
	isEasy, isNormal := d == Easy, d == Normal
	var bonusSpawns int
	switch k {
	case Vindicator, Pillager:
		if isEasy {
			bonusSpawns = r.IntN(2)
		} else if isNormal {
			bonusSpawns = 1
		} else {
			bonusSpawns = 2
		}
	case Witch:
		if isEasy || wave <= 2 || wave == 4 {
			return 0
		}
		bonusSpawns = 1
	case Ravager:
		if !isEasy && bonus {
			bonusSpawns = 1
		}
	default:
		return 0
	}
	if bonusSpawns > 0 {
		return r.IntN(bonusSpawns + 1)
	}
	return 0
}

// Member is one raider of a wave: its kind, the kind riding it (a ravager's rider, NoKind for none) and whether it
// is the wave's leader, the raid captain (Raid.setLeader).
type Member struct {
	Kind   Kind
	Rider  Kind
	Leader bool
}

// Composition is the raiders Raid.spawnGroup spawns for wave (groupNumber, from 1): the default spawns of each kind
// plus its potential bonus spawns, in spawn order. The first raider that can be a leader is the captain. A ravager
// of the wave Normal ends on carries a pillager; from the wave Hard ends on, the first ravager carries an evoker
// and the others a vindicator. bonus is the bonus wave of a raid with a raid omen above I; d is the difficulty of
// the regional difficulty at the spawn position.
func Composition(r *rand.Rand, d Difficulty, wave, numGroups int, bonus bool) []Member {
	var members []Member
	leaderSet := false
	for _, k := range Kinds {
		n := defaultNumSpawns(k, wave, numGroups, bonus) + potentialBonusSpawns(k, r, wave, d, bonus)
		ravagers := 0
		for i := 0; i < n; i++ {
			m := Member{Kind: k, Rider: NoKind}
			if !leaderSet && k.CanBeLeader() {
				m.Leader, leaderSet = true, true
			}
			if k == Ravager {
				if wave == NumGroups(Normal) {
					m.Rider = Pillager
				} else if wave >= NumGroups(Hard) {
					if ravagers == 0 {
						m.Rider = Evoker
					} else {
						m.Rider = Vindicator
					}
				}
				ravagers++
			}
			members = append(members, m)
		}
	}
	return members
}
