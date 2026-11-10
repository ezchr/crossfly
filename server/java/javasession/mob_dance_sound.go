package javasession

import "strings"

// javaImitations are the Java names of the Bedrock parrot imitations whose mob Java calls
// differently; "" for those Java has no imitation of (the parrot is silent for Java then).
var javaImitations = map[string]string{
	"cave_spider":         "spider", // Java's cave spider imitation is the spider's
	"evocation_illager":   "evoker",
	"vindication_illager": "vindicator",
	"illusion_illager":    "illusioner",
	"zombie_pigman":       "",
	"enderman":            "",
	"polar_bear":          "",
	"wolf":                "",
	"happy_ghast":         "",
}

// javaDanceSoundName maps the Bedrock mob sound events of allays and parrots (server/mob
// goal/misc/social) that javaMobSoundName's generic entity.<type>.<event> rule gets wrong: a
// parrot imitating a mob (imitate.<mob>, Java entity.parrot.imitate.<mob>) and an allay
// duplicating (chime.amethyst_block, Java block.amethyst_block.chime). handled is false for any
// other event; name is "" for an imitation Java has no sound for.
func javaDanceSoundName(event, typ string) (name string, handled bool) {
	switch {
	case typ == "minecraft:parrot" && strings.HasPrefix(event, "imitate."):
		m := strings.TrimPrefix(event, "imitate.")
		if j, ok := javaImitations[m]; ok {
			if j == "" {
				return "", true
			}
			m = j
		}
		return "minecraft:entity.parrot.imitate." + m, true
	case event == "chime.amethyst_block":
		return "minecraft:block.amethyst_block.chime", true
	}
	return "", false
}
