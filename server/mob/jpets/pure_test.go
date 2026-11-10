//go:build crossfly_full

package jpets

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

func newRand() *rand.Rand { return rand.New(rand.NewPCG(1, 2)) }

func TestHorseStatRanges(t *testing.T) {
	if MinHorseHealth != 15 || MaxHorseHealth != 30 {
		t.Fatalf("health range %v..%v, want 15..30", MinHorseHealth, MaxHorseHealth)
	}
	// AbstractHorse.MIN/MAX_MOVEMENT_SPEED are (float) of 0.1125 and 0.3375.
	if MinHorseSpeed != float64(float32(0.1125)) || math.Abs(MaxHorseSpeed-0.3375) > 1e-6 {
		t.Fatalf("speed range %v..%v", MinHorseSpeed, MaxHorseSpeed)
	}
	if math.Abs(MinHorseJump-0.4) > 1e-6 || math.Abs(MaxHorseJump-1.0) > 1e-6 {
		t.Fatalf("jump range %v..%v", MinHorseJump, MaxHorseJump)
	}
	r := newRand()
	for range 2000 {
		var a HorseAttributes
		RandomizeHorse(r, &a)
		if a.MaxHealth < 15 || a.MaxHealth > 30 || a.MaxHealth != math.Trunc(a.MaxHealth) {
			t.Fatalf("health %v", a.MaxHealth)
		}
		if a.Speed < MinHorseSpeed || a.Speed > MaxHorseSpeed+1e-9 || a.JumpStrength < MinHorseJump-1e-9 || a.JumpStrength > MaxHorseJump+1e-9 {
			t.Fatalf("stats %+v", a)
		}
	}
}

func TestOffspringAttributeStaysInRange(t *testing.T) {
	r := newRand()
	sum := 0.0
	const n = 20000
	for range n {
		v := OffspringAttribute(30, 30, MinHorseHealth, MaxHorseHealth, r)
		if v < MinHorseHealth || v > MaxHorseHealth {
			t.Fatalf("out of range: %v", v)
		}
		sum += v
	}
	// Two 30-health parents: the average 30 plus a spread of 4.5 around it,
	// mirrored at 30, so the foals average below 30 but well above 28.
	if avg := sum / n; avg < 28.5 || avg >= 30 {
		t.Fatalf("average foal health %v", avg)
	}
	// Values outside the range are clamped first.
	if v := OffspringAttribute(100, 100, 0, 1, r); v < 0 || v > 1 {
		t.Fatalf("clamped parents gave %v", v)
	}
}

func TestHandleEating(t *testing.T) {
	gc, _ := HorseFoodOf("minecraft:golden_carrot", false)
	// Tamed adult at full health: love, +5 temper (Used, so temper rises).
	res := HandleEating(gc, EatState{Health: 20, MaxHealth: 20, Tamed: true, Temper: 0, MaxTemper: 100}, false)
	if !res.Love || !res.Used || res.NewTemper != 5 || res.Heal != 0 {
		t.Fatalf("golden carrot: %+v", res)
	}
	// Wild horse, full health, wheat: only temper (not tamed).
	w, _ := HorseFoodOf("minecraft:wheat", false)
	res = HandleEating(w, EatState{Health: 20, MaxHealth: 20, Temper: 98, MaxTemper: 100}, false)
	if !res.Used || res.NewTemper != 100 {
		t.Fatalf("wheat wild: %+v", res)
	}
	// Tamed, full health, adult, wheat: nothing happens, item kept.
	res = HandleEating(w, EatState{Health: 20, MaxHealth: 20, Tamed: true, MaxTemper: 100}, false)
	if res.Used {
		t.Fatalf("wheat on a tamed healthy adult was eaten: %+v", res)
	}
	// Hay on a hurt baby: heal 20, age 180, no temper.
	h, _ := HorseFoodOf("minecraft:hay_block", false)
	res = HandleEating(h, EatState{Health: 5, MaxHealth: 20, Baby: true, MaxTemper: 100}, false)
	if res.Heal != 20 || res.AgeUp != 180 || res.NewTemper != 0 || !res.Used {
		t.Fatalf("hay baby: %+v", res)
	}
	// Llama hay: love only when it can fall in love.
	lh, _ := HorseFoodOf("minecraft:hay_block", true)
	res = HandleEating(lh, EatState{Health: 20, MaxHealth: 20, Tamed: true, CanFallInLove: true, MaxTemper: 30}, true)
	if !res.Love || res.NewTemper != 6 {
		t.Fatalf("llama hay: %+v", res)
	}
	if _, ok := HorseFoodOf("minecraft:golden_carrot", true); ok {
		t.Fatal("llamas do not eat golden carrots")
	}
}

func TestTameByRiding(t *testing.T) {
	r := newRand()
	if TameByRiding(r, 0, 100) {
		t.Fatal("temper 0 never tames")
	}
	if !TameByRiding(r, 100, 100) {
		t.Fatal("full temper always tames")
	}
	hits := 0
	for range 10000 {
		if TameByRiding(r, 50, 100) {
			hits++
		}
	}
	if hits < 4500 || hits > 5500 {
		t.Fatalf("temper 50 tamed %d/10000", hits)
	}
}

func TestJumpScale(t *testing.T) {
	if PlayerJumpPendingScale(90) != 1 || PlayerJumpPendingScale(100) != 1 {
		t.Fatal("full bar")
	}
	if v := PlayerJumpPendingScale(0); math.Abs(v-0.4) > 1e-6 {
		t.Fatalf("empty bar %v", v)
	}
	vy, dx, dz := HorseJumpVelocity(0.7, 1, 1, 0, true, 0)
	if math.Abs(vy-0.7) > 1e-6 || math.Abs(dx) > 1e-6 || math.Abs(dz-0.4) > 1e-6 {
		t.Fatalf("jump %v %v %v", vy, dx, dz)
	}
	// Facing east (yaw -90) pushes +x.
	_, dx, dz = HorseJumpVelocity(0.7, 1, 1, 0, true, -90)
	if math.Abs(dx-0.4) > 1e-4 || math.Abs(dz) > 1e-4 {
		t.Fatalf("east push %v %v", dx, dz)
	}
}

func TestHorseLooks(t *testing.T) {
	r := newRand()
	counts := map[int]int{}
	for range 9000 {
		v, m := HorseFoalLook(r, 1, 2, 3, 4)
		counts[v]++
		if m < 0 || m >= HorseMarkings {
			t.Fatalf("markings %d", m)
		}
	}
	// 4/9 + 1/63 each parent.
	if counts[1] < 3700 || counts[1] > 4500 || counts[3] < 3700 || counts[3] > 4500 {
		t.Fatalf("coat counts %v", counts)
	}
	for _, c := range [][2]string{{"minecraft:horse", "minecraft:donkey"}, {"minecraft:donkey", "minecraft:horse"}} {
		if typ, _ := EquineFoalType(c[0], c[1]); typ != "minecraft:mule" {
			t.Fatalf("%v gave %v", c, typ)
		}
	}
	if _, ok := EquineFoalType("minecraft:mule", "minecraft:mule"); ok {
		t.Fatal("mules are sterile")
	}
}

func TestLlamaStrength(t *testing.T) {
	r := newRand()
	seen := map[int]int{}
	for range 20000 {
		s := LlamaRandomStrength(r)
		seen[s]++
	}
	// 4% x 1/5 for strengths 4 and 5 each: about 0.8%.
	if seen[4] < 100 || seen[4] > 250 || seen[1] < 6000 || seen[6] != 0 || seen[0] != 0 {
		t.Fatalf("strength counts %v", seen)
	}
	for range 1000 {
		s, v := LlamaBabyLook(r, 5, 5, 0, 3)
		if s < 1 || s > 5 || (v != 0 && v != 3) {
			t.Fatalf("baby %d %d", s, v)
		}
	}
}

func TestPandaGenes(t *testing.T) {
	r := newRand()
	counts := map[Gene]int{}
	for range 16000 {
		counts[RandomGene(r)]++
	}
	// 1/16 lazy, worried, playful, aggressive; 5/16 weak; 2/16 brown; 5/16 normal.
	want := map[Gene]float64{GeneLazy: 1, GeneWorried: 1, GenePlayful: 1, GeneAggressive: 1, GeneWeak: 5, GeneBrown: 2, GeneNormal: 5}
	for g, w := range want {
		if c := float64(counts[g]); math.Abs(c-w*1000) > 150 {
			t.Fatalf("gene %v: %v of 16000, want about %v", g, c, w*1000)
		}
	}
	if PandaPersonality(GeneBrown, GeneNormal) != GeneNormal || PandaPersonality(GeneBrown, GeneBrown) != GeneBrown {
		t.Fatal("recessive brown")
	}
	if PandaPersonality(GeneLazy, GeneWeak) != GeneLazy {
		t.Fatal("dominant lazy")
	}
	// Two brown pandas almost always have brown babies (both genes brown
	// unless a 1/32 mutation hits).
	brown := 0
	for range 1000 {
		b := PandaBabyGenes(r, PandaGenes{GeneBrown, GeneBrown}, &PandaGenes{GeneBrown, GeneBrown})
		if PandaPersonality(b.Main, b.Hidden) == GeneBrown {
			brown++
		}
	}
	if brown < 900 {
		t.Fatalf("brown babies %d/1000", brown)
	}
	if GeneByName("aggressive") != GeneAggressive || GeneByName("?") != GeneNormal || GeneWeak.String() != "weak" {
		t.Fatal("names")
	}
}

type ctx struct {
	biome     string
	structure string
	moon      float64
}

func (c ctx) Biome() string              { return c.biome }
func (c ctx) InStructure(id string) bool { return c.structure == id }
func (c ctx) MoonBrightness() float64    { return c.moon }

func TestVariantSelection(t *testing.T) {
	r := newRand()
	cases := map[string]string{
		"minecraft:forest":                  "woods",
		"minecraft:snowy_taiga":             "ashen",
		"minecraft:grove":                   "snowy",
		"minecraft:bamboo_jungle":           "rusty",
		"minecraft:windswept_savanna":       "spotted",
		"minecraft:wooded_badlands":         "striped",
		"minecraft:old_growth_pine_taiga":   "black",
		"minecraft:old_growth_spruce_taiga": "chestnut",
		"minecraft:taiga":                   "pale",
		"minecraft:plains":                  "pale",
	}
	for biome, want := range cases {
		if got := WolfSpawnVariant(r, ctx{biome: biome}); got != want {
			t.Errorf("wolf in %v: %v, want %v", biome, got, want)
		}
	}
	for range 50 {
		if v := CatSpawnVariant(r, ctx{structure: "minecraft:swamp_hut", moon: 0.5}); v != "all_black" {
			t.Fatalf("witch hut cat %v", v)
		}
	}
	allBlack := 0
	for range 12000 {
		v := CatSpawnVariant(r, ctx{moon: 0.25})
		if v == "all_black" {
			t.Fatal("all black cat outside a full moon")
		}
		if CatSpawnVariant(r, ctx{moon: 1}) == "all_black" {
			allBlack++
		}
	}
	// Full moon: 11 entries, all black once among them.
	if allBlack < 800 || allBlack > 1400 {
		t.Fatalf("full moon all black %d/12000", allBlack)
	}
	if ZombieNautilusSpawnVariant(r, ctx{biome: "minecraft:warm_ocean"}) != "warm" ||
		ZombieNautilusSpawnVariant(r, ctx{biome: "minecraft:ocean"}) != "temperate" {
		t.Fatal("zombie nautilus")
	}
	if WolfVariantID("woods") != 8 || WolfVariantName(1) != "ashen" || CatVariantID("all_black") != 9 || CatVariantName(99) != "black" {
		t.Fatal("bedrock ids")
	}
}

func TestMixDyes(t *testing.T) {
	r := newRand()
	if MixDyes(r, DyeWhite, DyeRed) != DyePink || MixDyes(r, DyeRed, DyeWhite) != DyePink {
		t.Fatal("pink")
	}
	if MixDyes(r, DyeBlue, DyeRed) != DyePurple || MixDyes(r, DyePink, DyePurple) != DyeMagenta {
		t.Fatal("purple, magenta")
	}
	for range 20 {
		if c := MixDyes(r, DyeRed, DyeBlack); c != DyeRed && c != DyeBlack {
			t.Fatalf("red black gave %d", c)
		}
	}
	if c, ok := DyeOfItem("minecraft:light_gray_dye"); !ok || c != DyeLightGray {
		t.Fatal("dye item")
	}
}

type tpWorld struct {
	walkable map[cube.Pos]bool
	leaves   map[cube.Pos]bool
}

func (w tpWorld) Walkable(p cube.Pos) bool { return w.walkable[p] }
func (w tpWorld) Leaves(p cube.Pos) bool   { return w.leaves[p] }
func (w tpWorld) Free(cube.Pos) bool       { return true }

func TestTeleportSpot(t *testing.T) {
	r := newRand()
	owner := cube.Pos{0, 64, 0}
	w := tpWorld{walkable: map[cube.Pos]bool{}, leaves: map[cube.Pos]bool{}}
	for x := -3; x <= 3; x++ {
		for z := -3; z <= 3; z++ {
			w.walkable[cube.Pos{x, 64, z}] = true
		}
	}
	for range 200 {
		p, ok := TeleportSpot(r, owner, false, w)
		if !ok {
			continue
		}
		if p[1] != 64 || (iabs(p[0]) < 2 && iabs(p[2]) < 2) {
			t.Fatalf("bad spot %v", p)
		}
	}
	// Everything on leaves: walkers never land, fliers do.
	for x := -3; x <= 3; x++ {
		for z := -3; z <= 3; z++ {
			w.leaves[cube.Pos{x, 63, z}] = true
		}
	}
	for range 50 {
		if _, ok := TeleportSpot(r, owner, false, w); ok {
			t.Fatal("teleported onto leaves")
		}
	}
	landed := false
	for range 50 {
		if _, ok := TeleportSpot(r, owner, true, w); ok {
			landed = true
		}
	}
	if !landed {
		t.Fatal("a parrot may land on leaves")
	}
}

func TestSitRules(t *testing.T) {
	if !SitWhenOrderedTo(false, true, false, true, false, 0, false) {
		t.Fatal("a tame pet without its owner sits")
	}
	if SitWhenOrderedTo(true, true, false, true, true, 4, true) {
		t.Fatal("ordered to sit but the owner nearby was just hurt: stands")
	}
	if !SitWhenOrderedTo(true, true, false, true, true, 400, true) {
		t.Fatal("owner far away: keeps sitting")
	}
	if SitWhenOrderedTo(true, true, true, true, true, 4, false) {
		t.Fatal("never sits in water")
	}
	if !(PetMobility{Leashed: true}).UnableToMoveToOwner() || (PetMobility{}).UnableToMoveToOwner() {
		t.Fatal("mobility")
	}
}

func TestMthSin(t *testing.T) {
	for _, deg := range []float64{0, 30, 90, 180, -90, 270} {
		want := math.Sin(deg * math.Pi / 180)
		if got := float64(sinDeg32(deg)); math.Abs(got-want) > 1e-3 {
			t.Fatalf("sin %v = %v, want %v", deg, got, want)
		}
	}
}
