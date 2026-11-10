//go:build crossfly_full

package javamob

import (
	"encoding/binary"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/go-gl/mathgl/mgl64"
)

// state is the Java state of one mob: the mob.Component its definition attaches (definition.go). It lives as long
// as the mob and is saved with it.
type state struct {
	t     *Type
	b     Behaviour
	attrs *Attributes
	cur   *Mob

	goals, targets GoalSelector
	look           LookControl
	move           MoveControl
	jump           JumpControl
	body           BodyRotationControl
	nav            *path.Navigation
	sensing        Sensing
	brain          Brain

	malus    [path.TypeCount]float32
	malusSet [path.TypeCount]bool

	width, height, eyeHeight float64

	// Movement input and state (LivingEntity, Entity).
	zza, xxa, yya, speed float64
	jumping              bool
	noJumpDelay          int
	old                  mgl64.Vec3

	onGround, horizontalCollision, verticalCollision, verticalCollisionBelow, minorHorizontalCollision bool
	supporting                                                                                         *cube.Pos
	onGroundNoBlocks                                                                                   bool
	fallDistance                                                                                       float64
	stuck                                                                                              mgl64.Vec3

	inWater, eyeInWater, wasEyeInWater bool
	waterHeight, lavaHeight            float64
	inPowderSnow, wasInPowderSnow      bool
	firstTick                          bool

	tickCount         int
	idParity          int
	noActionTime      int
	hurtTime          int
	ambientSound      int
	lastHurtByMobTime int
	lastHurt          float64
	// numCollisions is Paper's Entity.numCollisions (pushEntities).
	numCollisions int
	homeRadius    int
	useStart      int
	group         *any

	persistenceRequired, canPickUpLoot, leftHanded, noAI bool
	dropChances                                          dropChances
	lootTable                                            string
	reason                                               SpawnReason
	finalized                                            bool
	setUp                                                bool

	boxes []cube.BBox
}

func newState(t *Type) *state {
	s := &state{t: t, b: t.New(), firstTick: true}
	bases := LivingAttributes()
	if t.Attributes != nil {
		bases = t.Attributes()
	}
	s.attrs = NewAttributes(bases)
	s.width, s.height, s.eyeHeight = t.Width, t.Height, t.EyeHeight
	s.dropChances = defaultDropChances()
	s.lootTable = t.LootTable
	// The engine's spawn event (spawn egg, /summon) finalizes with this unless Spawn set another reason.
	s.reason = SpawnSpawnItemUse
	s.homeRadius = -1
	return s
}

// Attach sets the mob up: its Bedrock-side state, controls, navigation and goals (Mob's constructor).
func (s *state) Attach(bm *mob.Mob) {
	m := &Mob{Mob: bm, s: s}
	s.cur = m
	if s.setUp {
		return
	}
	s.setUp = true
	id := bm.H().UUID()
	s.idParity = int(binary.LittleEndian.Uint32(id[12:]) & 1)
	s.old = bm.Position()
	bm.SetFamilies(s.t.Families)
	bm.UpdateMeta(func(meta *mob.Meta) { meta.Width, meta.Height, meta.Scale = s.width, s.height, 1 })
	bm.SetMaxHealth(s.attrs.Value(AttrMaxHealth))
	bm.SetHealth(s.attrs.Value(AttrMaxHealth))
	bm.SetSpeed(s.attrs.Value(AttrMovementSpeed))
	if s.t.NotInPeaceful {
		bm.SetPersistent(false)
	}
	s.look = LookControl{}
	s.move = MoveControl{}
	s.body = BodyRotationControl{}
	s.nav = path.NewNavigation(path.Ground, m)
	s.b.RegisterGoals(m)
}

// Detach ...
func (s *state) Detach(*mob.Mob) {}
