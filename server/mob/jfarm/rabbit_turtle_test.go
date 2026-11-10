//go:build crossfly_full

package jfarm

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/biome"
)

func TestRabbitVariants(t *testing.T) {
	r := rng()
	count := map[RabbitVariant]int{}
	for i := 0; i < 100000; i++ {
		count[RandomRabbitVariant(r, biome.Plains{})]++
	}
	if count[RabbitBrown] < 48000 || count[RabbitBrown] > 52000 || count[RabbitSalt] < 38000 || count[RabbitSalt] > 42000 ||
		count[RabbitBlack] < 9000 || count[RabbitBlack] > 11000 {
		t.Fatalf("plains rabbits %v", count)
	}
	if RandomRabbitVariant(r, biome.Desert{}) != RabbitGold {
		t.Fatal("desert rabbit not gold")
	}
	if v := RandomRabbitVariant(r, biome.SnowyPlains{}); v != RabbitWhite && v != RabbitWhiteSplotched {
		t.Fatalf("snowy rabbit %v", v)
	}
	biomeHits := 0
	for i := 0; i < 20000; i++ {
		if RabbitOffspring(r, RabbitGold, RabbitBlack, RabbitBlack) == RabbitGold {
			biomeHits++
		}
	}
	if biomeHits < 800 || biomeHits > 1200 {
		t.Fatalf("%d biome variants in 20000 kits, want about 1000", biomeHits)
	}
	if RabbitVariantByID(7) != RabbitBrown || RabbitEvil.Bedrock() != 0 || RabbitVariantByID(99) != RabbitEvil {
		t.Fatal("variant ids")
	}
	n := int32(RabbitMoreCarrotsDelay)
	ticks := 0
	for n > 0 {
		n = TickMoreCarrots(r, n)
		ticks++
	}
	if ticks < 20 || ticks > 120 {
		t.Fatalf("carrot wait %d ticks", ticks)
	}
}

func TestTurtleRules(t *testing.T) {
	r := rng()
	for i := 0; i < 1000; i++ {
		if n := TurtleEggCount(r); n < 1 || n > 4 {
			t.Fatal(n)
		}
	}
	if !IsSand(block.Sand{}) || !IsSand(block.Sand{Red: true}) || IsSand(block.Dirt{}) || IsSand(nil) {
		t.Fatal("sand tag")
	}
	if x, _, z := TurtleHatchPos(cube.Pos{10, 64, 20}, 2); math.Abs(x-10.7) > 1e-9 || math.Abs(z-20.3) > 1e-9 {
		t.Fatal(x, z)
	}
}

func TestHiveRules(t *testing.T) {
	r := rng()
	o := NewOccupant("minecraft:bee", map[string]any{"HasNectar": uint8(1), "Pos": []float32{1, 2, 3}, "UUID": "x"})
	if o.MinTicksInHive != HiveMinTicksNectar || o.Data["Pos"] != nil || o.Data["UUID"] != nil {
		t.Fatalf("occupant %+v", o)
	}
	ticks := 0
	for !TickOccupant(&o) {
		ticks++
	}
	if ticks != HiveMinTicksNectar+1 {
		t.Fatalf("left after %d ticks", ticks)
	}
	plain := NewOccupant("minecraft:bee", map[string]any{})
	if plain.MinTicksInHive != HiveMinTicksNectarless {
		t.Fatal("nectarless stay")
	}
	twos := 0
	for i := 0; i < 10000; i++ {
		switch HoneyIncrease(r, 2) {
		case 4:
			twos++
		case 3:
		default:
			t.Fatal("honey step")
		}
	}
	if twos < 60 || twos > 150 || HoneyIncrease(r, 4) != 5 || HoneyIncrease(r, 5) != 5 {
		t.Fatalf("honey: %d double steps", twos)
	}
	p := ReleasePos(cube.Pos{0, 64, 0}, cube.North, 0.7, 0.6, false)
	if math.Abs(p[2]+0.4) > 1e-9 || math.Abs(p[1]-64.2) > 1e-9 || p[0] != 0.5 {
		t.Fatal(p)
	}
}
