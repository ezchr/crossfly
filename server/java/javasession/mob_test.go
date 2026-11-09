package javasession

import (
	"bytes"
	"encoding/hex"
	"math"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	jitem "github.com/df-mc/dragonfly/server/java/protocol/item"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/java/protocol/version"
	"github.com/df-mc/dragonfly/server/java/protocol/wire"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// fakeType is an entity type with only a Bedrock id.
type fakeType string

func (t fakeType) Open(*world.Tx, *world.EntityHandle, *world.EntityData) world.Entity { return nil }
func (t fakeType) EncodeEntity() string                                                { return string(t) }
func (fakeType) BBox(world.Entity) cube.BBox                                           { return cube.Box(-0.3, 0, -0.3, 0.3, 1, 0.3) }
func (fakeType) DecodeNBT(map[string]any, *world.EntityData)                           {}
func (fakeType) EncodeNBT(*world.EntityData) map[string]any                            { return nil }

type fakeConf struct{}

func (fakeConf) Apply(*world.EntityData) {}

// fakeMob implements every interface of mob_iface.go, as the mob package's Mob does.
type fakeMob struct {
	h        *world.EntityHandle
	pos      mgl64.Vec3
	rot      cube.Rotation
	body     float64
	head     float64
	baby     bool
	variant  int32
	mark     int32
	colour   int32
	colour2  int32
	sheared  bool
	saddled  bool
	chested  bool
	tamed    bool
	sitting  bool
	charged  bool
	ignited  bool
	climbing bool
	shaking  bool
	convert  bool
	angry    bool
	aggro    bool
	using    bool
	sneaking bool
	invis    bool
	fire     time.Duration
	tier     int32
	props    map[string]any
	health   float64
	scale    float64
	owner    *world.EntityHandle
	facing   cube.Direction
	motive   string
}

func newFakeMob(id string) *fakeMob {
	return &fakeMob{h: world.EntitySpawnOpts{}.New(fakeType(id), fakeConf{}), health: 10, scale: 1}
}

func (m *fakeMob) Close() error                      { return nil }
func (m *fakeMob) H() *world.EntityHandle            { return m.h }
func (m *fakeMob) Position() mgl64.Vec3              { return m.pos }
func (m *fakeMob) Rotation() cube.Rotation           { return m.rot }
func (m *fakeMob) BodyYaw() float64                  { return m.body }
func (m *fakeMob) HeadYaw() float64                  { return m.head }
func (m *fakeMob) Baby() bool                        { return m.baby }
func (m *fakeMob) Variant() int32                    { return m.variant }
func (m *fakeMob) MarkVariant() int32                { return m.mark }
func (m *fakeMob) Colour() int32                     { return m.colour }
func (m *fakeMob) Colour2() int32                    { return m.colour2 }
func (m *fakeMob) Sheared() bool                     { return m.sheared }
func (m *fakeMob) Saddled() bool                     { return m.saddled }
func (m *fakeMob) Chested() bool                     { return m.chested }
func (m *fakeMob) Tamed() bool                       { return m.tamed }
func (m *fakeMob) Sitting() bool                     { return m.sitting }
func (m *fakeMob) Charged() bool                     { return m.charged }
func (m *fakeMob) Ignited() bool                     { return m.ignited }
func (m *fakeMob) Climbing() bool                    { return m.climbing }
func (m *fakeMob) Shaking() bool                     { return m.shaking }
func (m *fakeMob) Converting() bool                  { return m.convert }
func (m *fakeMob) Angry() bool                       { return m.angry }
func (m *fakeMob) Aggressive() bool                  { return m.aggro }
func (m *fakeMob) UsingItem() bool                   { return m.using }
func (m *fakeMob) Sneaking() bool                    { return m.sneaking }
func (m *fakeMob) Invisible() bool                   { return m.invis }
func (m *fakeMob) OnFireDuration() time.Duration     { return m.fire }
func (m *fakeMob) TradeTier() int32                  { return m.tier }
func (m *fakeMob) ActorProperties() map[string]any   { return m.props }
func (m *fakeMob) Health() float64                   { return m.health }
func (m *fakeMob) MaxHealth() float64                { return 20 }
func (m *fakeMob) BodyArmour() item.Stack            { return item.Stack{} }
func (m *fakeMob) Scale() float64                    { return m.scale }
func (m *fakeMob) Owner() *world.EntityHandle        { return m.owner }
func (m *fakeMob) Facing() cube.Direction            { return m.facing }
func (m *fakeMob) Motive() string                    { return m.motive }
func (m *fakeMob) NameTag() string                   { return "" }
func (m *fakeMob) setVariant(v, mark int32) *fakeMob { m.variant, m.mark = v, mark; return m }
func (m *fakeMob) with(f func(m *fakeMob)) *fakeMob  { f(m); return m }
func (m *fakeMob) prop(k string, v any) *fakeMob     { m.props = map[string]any{k: v}; return m }
func (m *fakeMob) props2(k, v, k2, v2 string) *fakeMob {
	m.props = map[string]any{k: v, k2: v2}
	return m
}
func (m *fakeMob) colours(c, c2 int32) *fakeMob         { m.colour, m.colour2 = c, c2; return m }
func (m *fakeMob) healthy(h float64) *fakeMob           { m.health = h; return m }
func (m *fakeMob) scaled(s float64) *fakeMob            { m.scale = s; return m }
func (m *fakeMob) facingTo(d cube.Direction) *fakeMob   { m.facing = d; return m }
func (m *fakeMob) motiveOf(n string) *fakeMob           { m.motive = n; return m }
func (m *fakeMob) owned(h *world.EntityHandle) *fakeMob { m.owner = h; return m }
func (m *fakeMob) yaws(rot cube.Rotation, b, h float64) *fakeMob {
	m.rot, m.body, m.head = rot, b, h
	return m
}

func mobTestSession() *Session {
	s := fxTestSession()
	s.entityIDs = map[*world.EntityHandle]int32{}
	s.tracks = map[int32]*track{}
	s.modelParts = map[int32][]int32{}
	return s
}

// meta builds a set_entity_data body: entity id, then (index, serializer, value) entries from
// the Java 26.3 layout (SynchedEntityData.defineId order in the decompile), then 0xff.
type meta struct{ w wire.Writer }

func entityData(id int32) *meta { m := &meta{}; m.w.VarInt(id); return m }
func (m *meta) b(i byte, v byte) *meta {
	m.w.Byte(i)
	m.w.VarInt(0)
	m.w.Byte(v)
	return m
}
func (m *meta) i(i byte, v int32) *meta { m.w.Byte(i); m.w.VarInt(1); m.w.VarInt(v); return m }
func (m *meta) l(i byte, v int64) *meta { m.w.Byte(i); m.w.VarInt(2); m.w.VarLong(v); return m }
func (m *meta) f(i byte, v float32) *meta {
	m.w.Byte(i)
	m.w.VarInt(3)
	m.w.Float32(v)
	return m
}
func (m *meta) z(i byte, v bool) *meta { m.w.Byte(i); m.w.VarInt(8); m.w.Bool(v); return m }

// h is a holder entry: serializer ser, registry network id v.
func (m *meta) h(i byte, ser, v int32) *meta { m.w.Byte(i); m.w.VarInt(ser); m.w.VarInt(v); return m }
func (m *meta) raw(i byte, ser int32, vals ...int32) *meta {
	m.w.Byte(i)
	m.w.VarInt(ser)
	for _, v := range vals {
		m.w.VarInt(v)
	}
	return m
}
func (m *meta) end() string { m.w.Byte(0xff); return hex.EncodeToString(m.w.B) }

// mob is the start every Mob's data has: health (LivingEntity 9) and mob flags (Mob 15).
func mob(health float32, flags byte) *meta { return entityData(7).f(9, health).b(15, flags) }

func TestMobDataLayout(t *testing.T) {
	const (
		catVariant, cowVariant, wolfVariant, frogVariant = 21, 23, 25, 27
		pigVariant, pigSound, chickenVariant             = 28, 29, 30
		villagerData                                     = 18
	)
	tests := []struct {
		name string
		e    *fakeMob
		want string
	}{
		{"sheep red sheared baby", newFakeMob("minecraft:sheep").colours(14, 0).with(func(m *fakeMob) { m.sheared, m.baby = true, true }).healthy(8),
			mob(8, 0).z(16, true).b(18, 14|0x10).end()},
		{"cow warm", newFakeMob("minecraft:cow").prop("minecraft:climate_variant", "warm"),
			mob(10, 0).z(16, false).h(18, cowVariant, 2).end()}, // cow_variant: cold 0, temperate 1, warm 2
		{"pig cold big", newFakeMob("minecraft:pig").props2("minecraft:climate_variant", "cold", "minecraft:sound_variant", "big"),
			mob(10, 0).z(16, false).h(19, pigVariant, 0).h(20, pigSound, 0).end()}, // pig_sound_variant: big 0
		{"chicken temperate", newFakeMob("minecraft:chicken").prop("minecraft:climate_variant", "temperate"),
			mob(10, 0).z(16, false).h(18, chickenVariant, 1).end()},
		{"baby zombie after a target", newFakeMob("minecraft:zombie").with(func(m *fakeMob) { m.baby, m.aggro = true, true }),
			mob(10, 0x04).z(16, true).z(18, false).end()},
		{"husk turning into a drowned...", newFakeMob("minecraft:husk").with(func(m *fakeMob) { m.shaking = true }),
			mob(10, 0).z(16, false).z(18, true).end()},
		{"skeleton turning into a stray", newFakeMob("minecraft:skeleton").with(func(m *fakeMob) { m.shaking, m.aggro = true, true }),
			mob(10, 0x04).z(16, true).end()},
		{"spider climbing", newFakeMob("minecraft:spider").with(func(m *fakeMob) { m.climbing = true }),
			mob(10, 0).b(16, 0x01).end()},
		{"charged creeper swelling", newFakeMob("minecraft:creeper").with(func(m *fakeMob) { m.ignited, m.charged = true, true }),
			mob(10, 0).i(16, 1).z(17, true).end()},
		{"creeper idle", newFakeMob("minecraft:creeper"), mob(10, 0).i(16, -1).z(17, false).end()},
		{"angry tame sitting chestnut wolf", newFakeMob("minecraft:wolf").setVariant(3, 0).colours(3, 0).with(func(m *fakeMob) { m.tamed, m.sitting, m.angry = true, true, true }),
			mob(10, 0x04).z(16, false).b(18, 0x05).i(21, 3).l(22, math.MaxInt64).h(23, wolfVariant, 2).end()},
		{"tuxedo cat", newFakeMob("minecraft:cat").setVariant(1, 0).colours(14, 0),
			mob(10, 0).z(16, false).b(18, 0).h(20, catVariant, 1).i(23, 14).end()},
		{"cold frog", newFakeMob("minecraft:frog").setVariant(1, 0), mob(10, 0).z(16, false).h(18, frogVariant, 0).end()},
		{"chestnut horse white dots", newFakeMob("minecraft:horse").setVariant(2, 3).with(func(m *fakeMob) { m.tamed = true }),
			mob(10, 0).z(16, false).b(18, 0x02).i(19, 2|3<<8).end()},
		{"llama with chest", newFakeMob("minecraft:llama").setVariant(2, 0).with(func(m *fakeMob) { m.chested = true }),
			mob(10, 0).z(16, false).b(18, 0).z(19, true).i(21, 2).end()},
		{"wild axolotl", newFakeMob("minecraft:axolotl").setVariant(3, 0), mob(10, 0).z(16, false).i(18, 1).end()},
		{"cyan axolotl", newFakeMob("minecraft:axolotl").setVariant(1, 0), mob(10, 0).z(16, false).i(18, 3).end()},
		{"rabbit gold", newFakeMob("minecraft:rabbit").setVariant(4, 0), mob(10, 0).z(16, false).i(18, 4).end()},
		{"grey parrot", newFakeMob("minecraft:parrot").setVariant(4, 0), mob(10, 0).z(16, false).b(18, 0).i(20, 4).end()},
		{"snow librarian, journeyman", newFakeMob("minecraft:villager_v2").setVariant(5, 4).with(func(m *fakeMob) { m.tier = 2 }),
			mob(10, 0).z(16, false).raw(19, villagerData, 4, 9, 3).end()}, // snow 4, librarian 9, level 3
		{"zombie villager, desert butcher, being cured", newFakeMob("minecraft:zombie_villager_v2").setVariant(11, 1).with(func(m *fakeMob) { m.convert = true }),
			mob(10, 0).z(16, false).z(18, false).z(19, true).raw(20, villagerData, 0, 2, 1).end()},
		{"tropical fish", newFakeMob("minecraft:tropicalfish").setVariant(1, 2).colours(4, 11),
			mob(10, 0).i(17, 1|2<<8|4<<16|11<<24).end()},
		{"black shulker", newFakeMob("minecraft:shulker").setVariant(0, 0), mob(10, 0).b(18, 15).end()},
		{"purple shulker", newFakeMob("minecraft:shulker").setVariant(16, 0), mob(10, 0).b(18, 16).end()},
		{"big slime", newFakeMob("minecraft:slime").setVariant(4, 0), mob(10, 0).z(16, false).i(18, 4).end()},
		{"brown panda", newFakeMob("minecraft:panda").setVariant(4, 0), mob(10, 0).z(16, false).b(21, 4).b(22, 4).end()},
		{"large salmon", newFakeMob("minecraft:salmon").scaled(1.5), mob(10, 0).i(17, 2).end()},
		{"snow golem without pumpkin", newFakeMob("minecraft:snow_golem").with(func(m *fakeMob) { m.sheared = true }), mob(10, 0).b(16, 0).end()},
		{"snow golem", newFakeMob("minecraft:snow_golem"), mob(10, 0).b(16, 0x10).end()},
		{"baby piglin", newFakeMob("minecraft:piglin").with(func(m *fakeMob) { m.baby = true }), mob(10, 0).z(17, true).end()},
		{"pigman", newFakeMob("minecraft:zombie_pigman"), mob(10, 0).z(16, false).z(18, false).end()},
		{"witch drinking", newFakeMob("minecraft:witch").with(func(m *fakeMob) { m.using = true }), mob(10, 0).z(17, true).end()},
		{"blue wither skull", newFakeMob("minecraft:wither_skull_dangerous"), entityData(7).z(8, true).end()},
		{"burning skull painting", newFakeMob("minecraft:painting").motiveOf("BurningSkull"),
			entityData(7).h(9, 34, v777.RegistryID("minecraft:painting_variant", "minecraft:burning_skull")+1).end()},
	}
	for _, tc := range tests {
		s := mobTestSession()
		s.viewMobData(tc.e, 7)
		out := s.take()
		if len(out) != 1 || out[0].id != v777.ClientboundPlaySetEntityData {
			t.Errorf("%s: want one set_entity_data, got %d packets", tc.name, len(out))
			continue
		}
		if got := hex.EncodeToString(out[0].w.B); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, got, tc.want)
		}
		s.viewMobData(tc.e, 7)
		if n := len(s.take()); n != 0 {
			t.Errorf("%s: unchanged data sent again", tc.name)
		}
	}
}

// The same layout and registry ids on 26.2.
func TestMobDataLayout262(t *testing.T) {
	s := mobTestSession()
	s.ver = version.V776
	s.viewMobData(newFakeMob("minecraft:cow").prop("minecraft:climate_variant", "cold"), 7)
	out := s.take()
	if want := mob(10, 0).z(16, false).h(18, 23, 0).end(); len(out) != 1 || hex.EncodeToString(out[0].w.B) != want {
		t.Fatalf("26.2 cow: %s", dump(out))
	}
}

func TestMobDataChange(t *testing.T) {
	s := mobTestSession()
	m := newFakeMob("minecraft:sheep")
	s.viewMobData(m, 7)
	s.take()
	m.sheared = true
	s.viewMobData(m, 7)
	out := s.take()
	if len(out) != 1 || !bytes.HasSuffix(out[0].w.B, []byte{18, 0, 0x10, 0xff}) {
		t.Fatalf("sheared sheep: %d packets", len(out))
	}
	s.viewMobData(newFakeMob("minecraft:item"), 8) // no mob data
	if n := len(s.take()); n != 0 {
		t.Fatalf("item got mob data")
	}
}

func TestBedrockMobIDsResolve(t *testing.T) {
	// Every entity of the BDS 1.26.63 vanilla behaviour pack.
	ids := "agent allay area_effect_cloud armadillo armor_stand arrow axolotl bat bee blaze boat bogged breeze " +
		"breeze_wind_charge_projectile camel camel_husk cat cave_spider chest_boat chest_minecart chicken cod " +
		"command_block_minecart copper_golem cow creaking creeper cushion dolphin donkey dragon_fireball drowned egg " +
		"elder_guardian ender_crystal ender_dragon ender_pearl enderman endermite evocation_illager eye_of_ender_signal " +
		"fireball fireworks_rocket fishing_hook fox frog ghast glow_squid goat guardian happy_ghast hoglin hopper_minecart " +
		"horse husk iron_golem lightning_bolt lingering_potion llama llama_spit magma_cube minecart mooshroom mule " +
		"nautilus npc ocelot ominous_item_spawner panda parched parrot phantom pig piglin piglin_brute pillager player " +
		"polar_bear pufferfish rabbit ravager salmon sheep shulker shulker_bullet silverfish skeleton skeleton_horse slime " +
		"small_fireball sniffer snow_golem snowball spider splash_potion squid stray strider sulfur_cube tadpole " +
		"thrown_trident tnt tnt_minecart trader_llama tripod_camera tropicalfish turtle vex villager villager_v2 " +
		"vindicator wandering_trader warden wind_charge_projectile witch wither wither_skeleton wither_skull " +
		"wither_skull_dangerous wolf xp_bottle xp_orb zoglin zombie zombie_horse zombie_nautilus zombie_pigman " +
		"zombie_villager zombie_villager_v2 painting item falling_block leash_knot"
	hidden := map[string]bool{"agent": true, "npc": true, "tripod_camera": true}
	for _, id := range bytes.Fields([]byte(ids)) {
		e := newFakeMob("minecraft:" + string(id))
		_, _, ok := javaEntity(e)
		if ok == hidden[string(id)] {
			t.Errorf("%s: shown %v", id, ok)
		}
		name, _ := javaTypeName(e)
		k := mobKinds[name]
		if _, isMob := bedrockMobs[string(id)]; isMob && (k == nil || k.notMob) {
			t.Errorf("%s (%s): no mob data", id, name)
		}
	}
	for n := range mobKinds {
		if v777.BuiltinID("minecraft:entity_type", n) < 0 {
			t.Errorf("mob kind %s is no 26.3 entity type", n)
		}
	}
}

// bedrockMobs are the vanilla Bedrock ids that are Java Mobs.
var bedrockMobs = map[string]struct{}{
	"allay": {}, "armadillo": {}, "axolotl": {}, "bat": {}, "bee": {}, "blaze": {}, "bogged": {}, "breeze": {},
	"camel": {}, "camel_husk": {}, "cat": {}, "cave_spider": {}, "chicken": {}, "cod": {}, "copper_golem": {},
	"cow": {}, "creaking": {}, "creeper": {}, "dolphin": {}, "donkey": {}, "drowned": {}, "elder_guardian": {},
	"ender_dragon": {}, "enderman": {}, "endermite": {}, "evocation_illager": {}, "fox": {}, "frog": {}, "ghast": {},
	"glow_squid": {}, "goat": {}, "guardian": {}, "happy_ghast": {}, "hoglin": {}, "horse": {}, "husk": {},
	"iron_golem": {}, "llama": {}, "magma_cube": {}, "mooshroom": {}, "mule": {}, "nautilus": {}, "ocelot": {},
	"panda": {}, "parched": {}, "parrot": {}, "phantom": {}, "pig": {}, "piglin": {}, "piglin_brute": {},
	"pillager": {}, "polar_bear": {}, "pufferfish": {}, "rabbit": {}, "ravager": {}, "salmon": {}, "sheep": {},
	"shulker": {}, "silverfish": {}, "skeleton": {}, "skeleton_horse": {}, "slime": {}, "sniffer": {},
	"snow_golem": {}, "spider": {}, "squid": {}, "stray": {}, "strider": {}, "sulfur_cube": {}, "tadpole": {},
	"trader_llama": {}, "tropicalfish": {}, "turtle": {}, "vex": {}, "villager": {}, "villager_v2": {},
	"vindicator": {}, "wandering_trader": {}, "warden": {}, "witch": {}, "wither": {}, "wither_skeleton": {},
	"wolf": {}, "zoglin": {}, "zombie": {}, "zombie_horse": {}, "zombie_nautilus": {}, "zombie_pigman": {},
	"zombie_villager": {}, "zombie_villager_v2": {},
}

// addEntity reads an add_entity body: type, yaw, head yaw and data.
func addEntity(t *testing.T, b []byte) (typ int32, yaw, head byte, data int32, pos mgl64.Vec3) {
	t.Helper()
	r := wire.NewReader(b)
	r.VarInt()
	r.UUID()
	typ = r.VarInt()
	pos = mgl64.Vec3{r.Float64(), r.Float64(), r.Float64()}
	r.LpVec3()
	r.Byte() // pitch
	yaw, head = r.Byte(), r.Byte()
	data = r.VarInt()
	if r.Err != nil {
		t.Fatalf("add_entity: %v", r.Err)
	}
	return
}

func TestMobSpawn(t *testing.T) {
	s := mobTestSession()
	m := newFakeMob("minecraft:sheep").yaws(cube.Rotation{0, 0}, 90, 180)
	m.fire, m.invis = time.Second, true
	s.viewOtherEntity(m)
	out := s.take()
	if len(out) < 3 || out[0].id != v777.ClientboundPlayAddEntity {
		t.Fatalf("%d packets", len(out))
	}
	typ, yaw, head, _, _ := addEntity(t, out[0].w.B)
	if typ != v777.BuiltinID("minecraft:entity_type", "minecraft:sheep") || yaw != 64 || head != 128 {
		t.Fatalf("add_entity type %d yaw %d head %d", typ, yaw, head)
	}
	// Then its state: the mob data, and the shared flags (on fire 0x01, invisible 0x20) with pose
	// and hand state.
	var data, flags bool
	for _, p := range out[1:] {
		if p.id != v777.ClientboundPlaySetEntityData {
			continue
		}
		data = data || bytes.Contains(p.w.B, []byte{18, 0, 0}) // white wool
		flags = flags || bytes.HasPrefix(p.w.B[1:], []byte{0, 0, 0x21, 6, 20, 0, 8, 0, 0})
	}
	if !data || !flags {
		t.Fatalf("spawn state: mob data %v, flags %v", data, flags)
	}

	// Moving turns the body and the head apart.
	m.body, m.head = 90, 45
	s.positionSync(m, mgl64.Vec3{0.5, 0, 0}, m.rot, true)
	out = s.take()
	if len(out) != 2 || out[1].id != v777.ClientboundPlayRotateHead || out[1].w.B[1] != 32 {
		t.Fatalf("move: %d packets", len(out))
	}
	if out[0].id != v777.ClientboundPlayMoveEntityPos {
		t.Fatalf("move: packet %#x, want a move without turning the body", out[0].id)
	}
}

func TestBoatSpawn(t *testing.T) {
	s := mobTestSession()
	b := newFakeMob("minecraft:boat").setVariant(8, 0).yaws(cube.Rotation{90, 0}, 90, 90)
	s.viewOtherEntity(b)
	out := s.take()
	typ, yaw, _, _, _ := addEntity(t, out[0].w.B)
	if typ != v777.BuiltinID("minecraft:entity_type", "minecraft:cherry_boat") || yaw != 0 {
		t.Fatalf("boat type %d yaw %d", typ, yaw)
	}
	b = newFakeMob("minecraft:chest_boat").setVariant(42, 0)
	s.viewOtherEntity(b)
	typ, _, _, _, _ = addEntity(t, s.take()[0].w.B)
	if typ != v777.BuiltinID("minecraft:entity_type", "minecraft:oak_chest_boat") {
		t.Fatalf("unknown chest boat type %d", typ)
	}
}

func TestPaintingSpawn(t *testing.T) {
	s := mobTestSession()
	p := newFakeMob("minecraft:painting").facingTo(cube.East).motiveOf("Kebab")
	p.pos = mgl64.Vec3{3.97, 64.5, -1.5}
	s.viewOtherEntity(p)
	out := s.take()
	_, _, _, data, pos := addEntity(t, out[0].w.B)
	if data != 5 || pos != (mgl64.Vec3{3, 64, -2}) {
		t.Fatalf("painting data %d pos %v", data, pos)
	}
	p = newFakeMob("minecraft:painting") // facing north, the zero direction
	s.viewOtherEntity(p)
	if _, _, _, data, _ = addEntity(t, s.take()[0].w.B); data != 2 {
		t.Fatalf("painting facing north: data %d", data)
	}
}

func TestFishingHookSpawn(t *testing.T) {
	_, self, s := testWorld(t)
	s.entityIDs = map[*world.EntityHandle]int32{}
	s.tracks = map[int32]*track{}
	stranger := newFakeMob("minecraft:player")
	s.viewOtherEntity(newFakeMob("minecraft:fishing_hook").owned(stranger.H()))
	if n := len(s.take()); n != 0 {
		t.Fatalf("hook of an unknown player spawned (%d packets): the client would drop it", n)
	}
	s.viewOtherEntity(newFakeMob("minecraft:fishing_hook").owned(self))
	out := s.take()
	if len(out) == 0 {
		t.Fatalf("own hook not spawned")
	}
	if _, _, _, data, _ := addEntity(t, out[0].w.B); data != selfEntityID {
		t.Fatalf("hook data %d, want the owner %d", data, selfEntityID)
	}
}

func TestMobSaddle(t *testing.T) {
	s := mobTestSession()
	m := newFakeMob("minecraft:pig")
	s.viewMobEquipment(m, 7)
	if n := len(s.take()); n != 0 {
		t.Fatalf("unsaddled pig sent equipment")
	}
	m.saddled = true
	s.viewMobEquipment(m, 7)
	out := s.take()
	id, _ := jitem.ByName("minecraft:saddle")
	var want wire.Writer
	want.VarInt(7)
	want.Byte(equipBody | 0x80)
	jitem.WriteEmpty(&want)
	want.Byte(equipSaddle)
	js := jitem.Stack{Count: 1, ID: id}
	js.Encode(&want)
	if len(out) != 1 || out[0].id != v777.ClientboundPlaySetEquipment || !bytes.Equal(out[0].w.B, want.B) {
		t.Fatalf("saddle: %s", dump(out))
	}
	s.viewMobEquipment(m, 7)
	if n := len(s.take()); n != 0 {
		t.Fatalf("unchanged saddle sent again")
	}
}

type eatGrassAction struct{}

func (eatGrassAction) EntityAction()     {}
func (eatGrassAction) ActorEvent() uint8 { return packet.ActorEventEatGrass }

func TestMobActorEvent(t *testing.T) {
	s := mobTestSession()
	m := newFakeMob("minecraft:sheep")
	s.entityIDs[m.H()] = 9
	s.ViewEntityAction(m, eatGrassAction{})
	out := s.take()
	if len(out) != 1 || out[0].id != v777.ClientboundPlayEntityEvent || hex.EncodeToString(out[0].w.B) != "000000090a" {
		t.Fatalf("eat grass: %s", dump(out))
	}
}

// dump shows packets as id: body hex.
func dump(out []outPacket) string {
	var b bytes.Buffer
	for _, p := range out {
		b.WriteString(hex.EncodeToString([]byte{byte(p.id)}) + ":" + hex.EncodeToString(p.w.B) + " ")
	}
	return b.String()
}
