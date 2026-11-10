//go:build crossfly_full

package boss

import (
	"encoding/binary"
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The End's features the fight builds: the obsidian spikes with their end
// crystals (SpikeFeature, TheEndSpikeHelper), the exit portal
// (EndPodiumFeature), the gateways (EndGatewayFeature, EndGatewayBlockActor)
// and the islands their exits stand on (EndIslandFeature). Dragonfly
// generates no End terrain, so these are placed by the fight, by the same
// rules BDS generates them with.

// mt19937 is Core::Random: a 32-bit Mersenne Twister.
type mt19937 struct {
	mt  [624]uint32
	idx int
}

func newMT(seed uint32) *mt19937 {
	r := &mt19937{idx: 624}
	r.mt[0] = seed
	for i := 1; i < 624; i++ {
		r.mt[i] = 1812433253*(r.mt[i-1]^(r.mt[i-1]>>30)) + uint32(i)
	}
	return r
}

// next is Core::Random::_genRandInt32.
func (r *mt19937) next() uint32 {
	if r.idx >= 624 {
		for i := range 624 {
			y := (r.mt[i] & 0x80000000) | (r.mt[(i+1)%624] & 0x7fffffff)
			v := r.mt[(i+397)%624] ^ (y >> 1)
			if y&1 != 0 {
				v ^= 0x9908b0df
			}
			r.mt[i] = v
		}
		r.idx = 0
	}
	y := r.mt[r.idx]
	r.idx++
	y ^= y >> 11
	y ^= (y << 7) & 0x9d2c5680
	y ^= (y << 15) & 0xefc60000
	y ^= y >> 18
	return y
}

// intn is Random::nextInt(n).
func (r *mt19937) intn(n int) int { return int(r.next() % uint32(n)) }

// shuffle is the Fisher-Yates shuffle of TheEndSpikeHelper::getSpikesForLevel
// (swap element i with one of 0..i). Java fallback, no Bedrock data: BDS
// shuffles the gateways with brstd::shuffle, whose draw is not in the dump;
// the gateways use this one too.
func (r *mt19937) shuffle(s []int) {
	for i := 1; i < len(s); i++ {
		j := r.intn(i + 1)
		s[i], s[j] = s[j], s[i]
	}
}

// worldSeed is the level seed (Level::getSeed, the low 32 bits of
// RandomSeed) of worlds whose provider keeps one, else 0.
func worldSeed(w *world.World) uint32 {
	if p, ok := w.Provider().(interface{ Seed() int64 }); ok {
		return uint32(p.Seed())
	}
	return 0
}

// EndSpike is SpikeFeature::EndSpike: an obsidian spike of the End.
type EndSpike struct {
	X, Z, Radius, Height int
	// Guarded spikes have an iron bar cage around their crystal.
	Guarded bool
}

// topBox is EndSpike::getTopBoundingBox: the column of the spike up to y 128,
// where its crystal is looked for.
func (s EndSpike) topBox() cube.BBox {
	return cube.Box(float64(s.X-s.Radius), 0, float64(s.Z-s.Radius), float64(s.X+s.Radius), 128, float64(s.Z+s.Radius))
}

// EndSpikes is TheEndSpikeHelper::getSpikesForLevel: ten spikes on a ring of
// 42 around the origin; spike i takes size n (a shuffle of 0..9 by the
// level seed): radius n/3+2, height 76+3n, caged for n 1 and 2.
func EndSpikes(seed uint32) []EndSpike {
	order := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	newMT(seed).shuffle(order)
	spikes := make([]EndSpike, 10)
	for i, n := range order {
		a := float64(float32(i)*0.62831855 - 6.2831855)
		spikes[i] = EndSpike{
			X:       int(float32(math.Cos(a) * 42)),
			Z:       int(float32(math.Sin(a) * 42)),
			Radius:  n/3 + 2,
			Height:  76 + 3*n,
			Guarded: n == 1 || n == 2,
		}
	}
	return spikes
}

// placeSpike is SpikeFeature::place then postProcessMobsAt: the obsidian
// column (from the bottom of the world), air above y 65 around it, the cage
// of guarded spikes, then the crystal on a burning bedrock block on top. The
// crystal gets the beam target and invulnerability passed (the respawn).
func placeSpike(tx *world.Tx, s EndSpike, invulnerable bool, beam *cube.Pos) *world.EntityHandle {
	ob, air := block.Obsidian{}, block.Air{}
	r2 := s.Radius*s.Radius + 1
	bottom := tx.Range()[0]
	for x := s.X - s.Radius; x <= s.X+s.Radius; x++ {
		for z := s.Z - s.Radius; z <= s.Z+s.Radius; z++ {
			dx, dz := x-s.X, z-s.Z
			inside := dx*dx+dz*dz <= r2
			for y := bottom; y <= s.Height+10; y++ {
				pos := cube.Pos{x, y, z}
				switch {
				case inside && y < s.Height:
					tx.SetBlock(pos, ob, nil)
				case y > 65:
					tx.SetBlock(pos, air, nil)
				}
			}
		}
	}
	if s.Guarded {
		bars := block.IronBars{}
		for dx := -2; dx <= 2; dx++ {
			for dz := -2; dz <= 2; dz++ {
				p := cube.Pos{s.X + dx, s.Height, s.Z + dz}
				if dx == -2 || dx == 2 || dz == -2 || dz == 2 {
					for dy := 0; dy < 3; dy++ {
						tx.SetBlock(p.Add(cube.Pos{0, dy, 0}), bars, nil)
					}
				}
				tx.SetBlock(p.Add(cube.Pos{0, 3, 0}), bars, nil)
			}
		}
	}
	tx.SetBlock(cube.Pos{s.X, s.Height, s.Z}, block.Bedrock{InfiniteBurning: true}, nil)
	return spawnCrystal(tx, mgl64.Vec3{float64(s.X) + 0.5, float64(s.Height + 1), float64(s.Z) + 0.5}, invulnerable, beam)
}

// spawnCrystal adds an end crystal showing its base.
func spawnCrystal(tx *world.Tx, pos mgl64.Vec3, invulnerable bool, beam *cube.Pos) *world.EntityHandle {
	h := entity.EndCrystalConfig{ShowBase: true}.New(world.EntitySpawnOpts{Position: pos})
	e := tx.AddEntity(h)
	if invulnerable {
		entity.SetEndCrystalInvulnerable(e, true)
	}
	if beam != nil {
		entity.SetEndCrystalBeam(e, *beam, true)
	}
	return h
}

// spikesMissing reports that the End has none of the spikes: no obsidian
// below any spike top.
func spikesMissing(tx *world.Tx, spikes []EndSpike) bool {
	for _, s := range spikes {
		if _, ok := tx.Block(cube.Pos{s.X, s.Height - 1, s.Z}).(block.Obsidian); ok {
			return false
		}
	}
	return true
}

// podiumBedrock is the bedrock of the exit portal and the gateways, placed
// with infiniburn_bit set as EndPodiumFeature and EndGatewayFeature do.
var podiumBedrock = block.Bedrock{InfiniteBurning: true}

// placePodium is EndPodiumFeature::place at origin (the level of the portal
// blocks): a bedrock bowl of radius 3.5 with end stone under its rim, air
// above up to 4 blocks, the 3.5-wide inside open or (active) filled with end
// portal blocks, the bedrock pillar of 4 and its four torches.
func placePodium(tx *world.Tx, origin cube.Pos, active bool) {
	for dx := -4; dx <= 4; dx++ {
		for dy := -1; dy <= 4; dy++ {
			for dz := -4; dz <= 4; dz++ {
				d := float64(dx*dx + dy*dy + dz*dz)
				if d >= 12.25 {
					continue
				}
				pos := origin.Add(cube.Pos{dx, dy, dz})
				switch {
				case dy < 0 && d < 6.25:
					tx.SetBlock(pos, podiumBedrock, nil)
				case dy < 0:
					tx.SetBlock(pos, block.EndStone{}, nil)
				case dy > 0:
					tx.SetBlock(pos, nil, nil)
				case d >= 6.25:
					tx.SetBlock(pos, podiumBedrock, nil)
				case active:
					tx.SetBlock(pos, block.EndPortal{}, nil)
				default:
					tx.SetBlock(pos, nil, nil)
				}
			}
		}
	}
	for i := range 4 {
		tx.SetBlock(origin.Add(cube.Pos{0, i, 0}), podiumBedrock, nil)
	}
	centre := origin.Add(cube.Pos{0, 2, 0})
	for _, f := range []cube.Face{cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast} {
		tx.SetBlock(centre.Side(f), block.Torch{Facing: f.Opposite(), Type: block.NormalFire()}, nil)
	}
}

// placeGateway is EndGatewayFeature::place: the gateway block in a frame of
// bedrock above and below it, the blocks around it cleared. exit, if set, is
// where the gateway sends entities.
func placeGateway(tx *world.Tx, pos cube.Pos, exit *cube.Pos) {
	for dx := -1; dx <= 1; dx++ {
		for dy := -2; dy <= 2; dy++ {
			for dz := -1; dz <= 1; dz++ {
				p := pos.Add(cube.Pos{dx, dy, dz})
				sameX, sameY, sameZ := dx == 0, dy == 0, dz == 0
				edgeY := dy == 2 || dy == -2
				switch {
				case sameX && sameY && sameZ:
					g := block.EndGateway{}
					if exit != nil {
						g.Exit, g.HasExit = *exit, true
					}
					tx.SetBlock(p, g, nil)
				case sameY:
					tx.SetBlock(p, nil, nil)
				case edgeY && sameX && sameZ:
					tx.SetBlock(p, podiumBedrock, nil)
				case (sameX || sameZ) && !edgeY:
					tx.SetBlock(p, podiumBedrock, nil)
				default:
					tx.SetBlock(p, nil, nil)
				}
			}
		}
	}
}

// setGatewayExit links the gateway at pos to exit.
func setGatewayExit(tx *world.Tx, pos, exit cube.Pos) {
	if g, ok := tx.Block(pos).(block.EndGateway); ok {
		g.Exit, g.HasExit = exit, true
		tx.SetBlock(pos, g, nil)
	}
}

// hash3 is mce::Math::hash3<int,int,int>, the seed of an exit island.
// Java fallback, no Bedrock data: the hash itself is not in the dump; this
// mixes the three coordinates the way Java's Mth.getSeed does.
func hash3(p cube.Pos) uint32 {
	x, y, z := int64(p[0]), int64(p[1]), int64(p[2])
	s := x*3129871 ^ z*116129781 ^ y
	s = s*s*42317861 + s*11
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(s>>16))
	return binary.LittleEndian.Uint32(b[:4])
}

// placeIsland is EndIslandFeature::place: discs of end stone shrinking
// downwards from pos, radius 4-6 losing 0.5-1.5 a layer.
func placeIsland(tx *world.Tx, pos cube.Pos, r *mt19937) {
	es := block.EndStone{}
	y := 0
	for rad := float64(r.intn(3)) + 4; rad > 0.5; rad -= 0.5 + float64(r.intn(2)) {
		lo, hi := int(math.Floor(-rad)), int(math.Ceil(rad))
		for x := lo; x <= hi; x++ {
			for z := lo; z <= hi; z++ {
				if float64(x*x+z*z) <= (rad+1)*(rad+1) {
					tx.SetBlock(pos.Add(cube.Pos{x, y, z}), es, nil)
				}
			}
		}
		y--
	}
}

// solidBlocking is Block::isSolidBlockingBlock, near enough: a block with a
// solid top that is not a liquid.
func solidBlocking(tx *world.Tx, pos cube.Pos) bool { return solidTop(tx, pos) }

func isBlock[T world.Block](tx *world.Tx, pos cube.Pos) bool {
	_, ok := tx.Block(pos).(T)
	return ok
}

// topSolidUnder is BlockSource::findNextTopSolidBlockUnder: the highest
// solid block at or under pos in its column.
func topSolidUnder(tx *world.Tx, pos cube.Pos) (cube.Pos, bool) {
	y := min(pos[1], tx.HighestBlock(pos[0], pos[2]))
	for ; y >= tx.Range()[0]; y-- {
		p := cube.Pos{pos[0], y, pos[2]}
		if solidBlocking(tx, p) {
			return p, true
		}
	}
	return cube.Pos{}, false
}

// findValidSpawnAround is EndGatewayBlockActor::findValidSpawnAround: the
// block nearest pos, within 16*r columns, that an entity can stand on top of
// (with endStone: the highest block is end stone; else two blocks of room
// above it and not bedrock, unless it is the bedrock over a gateway, when
// the ground below the gateway counts).
func findValidSpawnAround(tx *world.Tx, pos cube.Pos, endStone bool, r int) (cube.Pos, bool) {
	var best cube.Pos
	found, bestD := false, 0.0
	for x := pos[0] - 16*r; x <= pos[0]+16*r; x++ {
		for z := pos[2] - 16*r; z <= pos[2]+16*r; z++ {
			p, ok := topSolidUnder(tx, cube.Pos{x, 127, z})
			if !ok {
				continue
			}
			valid := false
			if isBlock[block.Bedrock](tx, p) {
				if !isBlock[block.EndGateway](tx, p.Sub(cube.Pos{0, 2, 0})) {
					continue
				}
				q, ok := topSolidUnder(tx, p.Sub(cube.Pos{0, 2, 0}))
				if !ok {
					continue
				}
				p = q
				valid = !solidBlocking(tx, p.Add(cube.Pos{0, 1, 0})) && !solidBlocking(tx, p.Add(cube.Pos{0, 2, 0}))
			} else if endStone {
				valid = isBlock[block.EndStone](tx, p)
			} else {
				valid = !solidBlocking(tx, p.Add(cube.Pos{0, 1, 0})) && !solidBlocking(tx, p.Add(cube.Pos{0, 2, 0}))
			}
			if !valid {
				continue
			}
			d := p.Vec3().Sub(pos.Vec3()).LenSqr()
			if !found || d < bestD {
				best, bestD, found = p, d, true
			}
		}
	}
	return best, found
}

// findTallestBlock is EndGatewayBlockActor::findTallestBlock: the highest
// solid block within r columns of pos (the centre column and bedrock only
// with allowBedrock), or pos.
func findTallestBlock(tx *world.Tx, pos cube.Pos, r int, allowBedrock bool) cube.Pos {
	best, found := pos, false
	bestY := math.MinInt
	for dx := -r; dx <= r; dx++ {
		for dz := -r; dz <= r; dz++ {
			if dx == 0 && dz == 0 && !allowBedrock {
				continue
			}
			x, z := pos[0]+dx, pos[2]+dz
			for y := tx.HighestBlock(x, z); y > bestY && y >= tx.Range()[0]; y-- {
				p := cube.Pos{x, y, z}
				if !solidBlocking(tx, p) || (!allowBedrock && isBlock[block.Bedrock](tx, p)) {
					continue
				}
				best, bestY, found = p, y, true
				break
			}
		}
	}
	if !found {
		return pos
	}
	return best
}

// hasTerrain is the test of EndGatewayBlockActor::_getHighestSection: the
// chunk of pos (generated if needed) has blocks. One column of it stands for
// the chunk.
func hasTerrain(tx *world.Tx, x, z int) bool {
	return tx.HighestBlock(x, z) > tx.Range()[0] || !isBlock[block.Air](tx, cube.Pos{x, tx.Range()[0], z})
}

// findExitPortal is EndGatewayBlockActor::findExitPortal: 1024 blocks out
// from the origin in the direction of pos, then back towards the origin in
// steps of 16 while there is land (at most 16), then out again while there
// is none (at most 16).
func findExitPortal(tx *world.Tx, pos cube.Pos) cube.Pos {
	dir := mgl64.Vec3{float64(pos[0]), 0, float64(pos[2])}
	if dir.Len() < 0.0001 {
		dir = mgl64.Vec3{0, 0, 1}
	} else {
		dir = dir.Normalize()
	}
	p := dir.Mul(1024)
	for i := 16; hasTerrain(tx, int(math.Floor(p[0])), int(math.Floor(p[2]))) && i > 0; i-- {
		p = p.Sub(dir.Mul(16))
	}
	for i := 16; !hasTerrain(tx, int(math.Floor(p[0])), int(math.Floor(p[2]))) && i > 0; i-- {
		p = p.Add(dir.Mul(16))
	}
	return cube.PosFromVec3(p)
}
