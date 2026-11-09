//go:build crossfly_full

package mob

import (
	"fmt"
	"maps"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// InteractHook is an interaction BDS gives some actor types in code instead
// of through their definition: Fish, Axolotl and Tadpole::initializeComponents
// set a BucketableDescription no behaviour pack file names. Interact reports
// whether it handled the use, like an Interactor component; hooks run after
// the mob's Interactor components and before its LateInteractor ones.
type InteractHook struct {
	Name     string
	Interact func(m *Mob, user item.User, held item.Stack, clickPos mgl64.Vec3) (item.Stack, bool)
}

var interactHooks []InteractHook

// RegisterInteractHook adds an interact hook. Call it from init only.
func RegisterInteractHook(h InteractHook) {
	if h.Name == "" || h.Interact == nil {
		panic(fmt.Sprintf("mob: invalid interact hook %+v", h))
	}
	for _, o := range interactHooks {
		if o.Name == h.Name {
			panic(fmt.Sprintf("mob: interact hook %q registered twice", h.Name))
		}
	}
	interactHooks = append(interactHooks, h)
}

// runInteractHooks runs the interact hooks for Mob.Interact.
func (m *Mob) runInteractHooks(user item.User, held item.Stack, clickPos mgl64.Vec3) (item.Stack, bool) {
	for _, h := range interactHooks {
		if left, done := h.Interact(m, user, held, clickPos); done {
			return left, true
		}
	}
	return held, false
}

// SaveData returns the mob saved the way a chunk saves it: its identifier,
// position, motion, rotation, fire, age and name tag, and the mob's own data
// (health, definition groups and properties, component state). SpawnSaved
// spawns it again. BDS BucketItem::addBucketEntitySaveData puts the actor's
// save data in a mob bucket the same way.
func (m *Mob) SaveData() map[string]any {
	d := m.data
	out := map[string]any{
		"identifier": m.s.t.id,
		"Pos":        []float32{float32(d.Pos[0]), float32(d.Pos[1]), float32(d.Pos[2])},
		"Motion":     []float32{float32(d.Vel[0]), float32(d.Vel[1]), float32(d.Vel[2])},
		"Yaw":        float32(d.Rot[0]),
		"Pitch":      float32(d.Rot[1]),
		"Fire":       int16(d.FireDuration.Seconds() * 20),
		"Age":        int16(min(d.Age.Seconds()*20, 32767)),
		"NameTag":    d.Name,
	}
	maps.Copy(out, m.s.t.EncodeNBT(d))
	return out
}

// SpawnSaved adds a mob saved with SaveData at pos, with a new unique id, as
// BDS BucketItem::readBucketEntitySaveData loads a bucket's actor: it keeps
// everything it was saved with and fires no spawn event.
func SpawnSaved(tx *world.Tx, data map[string]any, pos mgl64.Vec3) (*Mob, error) {
	id, _ := data["identifier"].(string)
	t, ok := TypeFor(id)
	if !ok {
		return nil, fmt.Errorf("mob: no mob type %q", id)
	}
	name, _ := data["NameTag"].(string)
	rot := cube.Rotation{float64(nbtconv.Float32(data, "Yaw")), float64(nbtconv.Float32(data, "Pitch"))}
	h := world.EntitySpawnOpts{Position: pos, Rotation: rot, NameTag: name}.New(t, savedConfig{t: t, data: data})
	m, ok := Of(tx.AddEntity(h))
	if !ok {
		return nil, fmt.Errorf("mob: %v did not open as a mob", id)
	}
	m.ensureInit()
	return m, nil
}

// savedConfig makes a handle that loads the mob from saved data, as a chunk
// load does.
type savedConfig struct {
	t    *Type
	data map[string]any
}

func (c savedConfig) Apply(data *world.EntityData) { c.t.DecodeNBT(c.data, data) }
