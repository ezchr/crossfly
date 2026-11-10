//go:build crossfly_full

package jpets

import "math/rand/v2"

// Inherited looks: horse coats, llama strength and colour, panda genes.
// Bedrock and Java number horse variants, markings and llama variants the
// same way, so these ids go straight into mob.Meta.Variant/MarkVariant.

// Horse coat colours (equine.Variant) and markings (equine.Markings).
const (
	HorseVariants = 7 // white creamy chestnut brown black gray dark_brown
	HorseMarkings = 5 // none white white_field white_dots black_dots
)

// HorseSpawnLook is Horse.finalizeSpawn: the group's variant (the first horse
// of a spawned group picks one at random) and random markings.
func HorseSpawnLook(r *rand.Rand, groupVariant int, hasGroup bool) (variant, markings int) {
	if hasGroup {
		variant = groupVariant
	} else {
		variant = r.IntN(HorseVariants)
	}
	return variant, r.IntN(HorseMarkings)
}

// HorseFoalLook is Horse.getBreedOffspring for two horses: the coat comes
// from this parent 4/9, the partner 4/9 or is random 1/9; the markings from
// this parent 2/5, the partner 2/5 or random 1/5.
func HorseFoalLook(r *rand.Rand, variant, markings, partnerVariant, partnerMarkings int) (int, int) {
	var v, m int
	switch s := r.IntN(9); {
	case s < 4:
		v = variant
	case s < 8:
		v = partnerVariant
	default:
		v = r.IntN(HorseVariants)
	}
	switch s := r.IntN(5); {
	case s < 2:
		m = markings
	case s < 4:
		m = partnerMarkings
	default:
		m = r.IntN(HorseMarkings)
	}
	return v, m
}

// EquineFoalType is the type of a foal (Horse/Donkey/Mule.getBreedOffspring):
// horse x horse = horse, donkey x donkey = donkey, horse x donkey = mule.
// The mob breeding is the first argument; mules never breed (canMate false).
func EquineFoalType(self, partner string) (string, bool) {
	switch {
	case self == "minecraft:horse" && partner == "minecraft:horse":
		return "minecraft:horse", true
	case self == "minecraft:donkey" && partner == "minecraft:donkey":
		return "minecraft:donkey", true
	case self == "minecraft:horse" && partner == "minecraft:donkey",
		self == "minecraft:donkey" && partner == "minecraft:horse":
		return "minecraft:mule", true
	case (self == "minecraft:llama" || self == "minecraft:trader_llama") &&
		(partner == "minecraft:llama" || partner == "minecraft:trader_llama"):
		// Llama.canMate takes any Llama (trader llamas are Llamas);
		// makeNewLlama makes a llama (TraderLlama.makeNewLlama a trader llama).
		if self == "minecraft:trader_llama" {
			return "minecraft:trader_llama", true
		}
		return "minecraft:llama", true
	}
	return "", false
}

// Llama variants (Llama.Variant): creamy 0, white 1, brown 2, gray 3.
const LlamaVariants = 4

// LlamaRandomStrength is Llama.setRandomStrength: 1..3, or 1..5 with a 4%
// chance.
func LlamaRandomStrength(r *rand.Rand) int {
	max := 3
	if r.Float32() < 0.04 {
		max = 5
	}
	return ClampLlamaStrength(1 + r.IntN(max))
}

// ClampLlamaStrength is Llama.setStrength: 1..5.
func ClampLlamaStrength(s int) int { return max(1, min(5, s)) }

// LlamaSpawnVariant is Llama.finalizeSpawn's colour: the group's, or random
// for the first of a group.
func LlamaSpawnVariant(r *rand.Rand, groupVariant int, hasGroup bool) int {
	if hasGroup {
		return groupVariant
	}
	return r.IntN(LlamaVariants)
}

// LlamaBabyLook is Llama.getBreedOffspring after setOffspringAttributes:
// strength nextInt(max of the parents)+1, +1 with a 3% chance (then clamped
// to 5), and either parent's colour.
func LlamaBabyLook(r *rand.Rand, strength, partnerStrength, variant, partnerVariant int) (int, int) {
	s := r.IntN(max(strength, partnerStrength)) + 1
	if r.Float32() < 0.03 {
		s++
	}
	v := partnerVariant
	if r.IntN(2) == 0 { // nextBoolean
		v = variant
	}
	return ClampLlamaStrength(s), v
}

// LlamaInventoryColumns is Llama.getInventoryColumns: strength columns of 3
// slots when chested.
func LlamaInventoryColumns(chested bool, strength int) int {
	if !chested {
		return 0
	}
	return strength
}

// Panda genes (Panda.Gene). Bedrock's panda variants use the same order.
type Gene uint8

const (
	GeneNormal Gene = iota
	GeneLazy
	GeneWorried
	GenePlayful
	GeneBrown
	GeneWeak
	GeneAggressive
)

var geneNames = [...]string{"normal", "lazy", "worried", "playful", "brown", "weak", "aggressive"}

// String is the gene's serialized name (MainGene/HiddenGene in NBT).
func (g Gene) String() string {
	if int(g) < len(geneNames) {
		return geneNames[g]
	}
	return "normal"
}

// GeneByName parses a serialized gene (normal if unknown, the codec's default).
func GeneByName(s string) Gene {
	for i, n := range geneNames {
		if n == s {
			return Gene(i)
		}
	}
	return GeneNormal
}

// Recessive reports whether the gene only shows when both genes are it.
func (g Gene) Recessive() bool { return g == GeneBrown || g == GeneWeak }

// RandomGene is Panda.Gene.getRandom: of nextInt(16), 0 lazy, 1 worried,
// 2 playful, 4 aggressive, 3 and 5..8 weak, 9..10 brown, else normal.
func RandomGene(r *rand.Rand) Gene {
	switch n := r.IntN(16); {
	case n == 0:
		return GeneLazy
	case n == 1:
		return GeneWorried
	case n == 2:
		return GenePlayful
	case n == 4:
		return GeneAggressive
	case n < 9:
		return GeneWeak
	case n < 11:
		return GeneBrown
	}
	return GeneNormal
}

// PandaPersonality is Panda.Gene.getVariantFromGenes: a recessive main gene
// shows only if the hidden gene is the same, else the panda is normal.
func PandaPersonality(main, hidden Gene) Gene {
	if main.Recessive() {
		if main == hidden {
			return main
		}
		return GeneNormal
	}
	return main
}

// PandaGenes is a panda's pair of genes.
type PandaGenes struct{ Main, Hidden Gene }

func (p PandaGenes) oneRandomly(r *rand.Rand) Gene {
	if r.IntN(2) == 0 {
		return p.Main
	}
	return p.Hidden
}

// PandaBabyGenes is Panda.setGeneFromParents (parent2 nil for a panda born
// without a partner: a sneezed baby has two parents too, so nil is rare).
// Either parent gives the main gene; each gene mutates with 1/32.
func PandaBabyGenes(r *rand.Rand, p1 PandaGenes, p2 *PandaGenes) PandaGenes {
	var b PandaGenes
	switch {
	case p2 == nil:
		if r.IntN(2) == 0 {
			b.Main = p1.oneRandomly(r)
			b.Hidden = RandomGene(r)
		} else {
			b.Main = RandomGene(r)
			b.Hidden = p1.oneRandomly(r)
		}
	case r.IntN(2) == 0:
		b.Main = p1.oneRandomly(r)
		b.Hidden = p2.oneRandomly(r)
	default:
		b.Main = p2.oneRandomly(r)
		b.Hidden = p1.oneRandomly(r)
	}
	if r.IntN(32) == 0 {
		b.Main = RandomGene(r)
	}
	if r.IntN(32) == 0 {
		b.Hidden = RandomGene(r)
	}
	return b
}
