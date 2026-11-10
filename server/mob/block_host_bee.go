//go:build crossfly_full

package mob

import (
	"maps"
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Compile time check that BlockHost lets beehives keep bees.
var _ block.BeehiveHost = BlockHost{}

// propertyHasNectar is the property bee.json sets while a bee carries nectar (Bee _beeHasNectar).
const propertyHasNectar = "minecraft:has_nectar"

// StoreMob saves the mob as the world saves an entity (the handle's Pos, Motion, Yaw, Pitch, Fire, Age and NameTag,
// the mob's own NBT and its identifier) and removes it, for a hive to keep (BeehiveBlockActor::tryAdmit).
func (BlockHost) StoreMob(_ *world.Tx, e world.Entity) (string, map[string]any, bool) {
	m, ok := Of(e)
	if !ok || m.Dead() || m.s.removed {
		return "", nil, false
	}
	m.ensureInit()
	d := m.data
	data := map[string]any{
		"Pos":     []float32{float32(d.Pos[0]), float32(d.Pos[1]), float32(d.Pos[2])},
		"Motion":  []float32{float32(d.Vel[0]), float32(d.Vel[1]), float32(d.Vel[2])},
		"Yaw":     float32(d.Rot[0]),
		"Pitch":   float32(d.Rot[1]),
		"Fire":    int16(d.FireDuration.Seconds() * 20),
		"Age":     int16(min(d.Age/(time.Second/20), math.MaxInt16)),
		"NameTag": d.Name,
	}
	maps.Copy(data, m.s.encodeNBT())
	data["identifier"] = m.Identifier()
	_ = m.Close()
	return m.Identifier(), data, true
}

// ReleaseMob adds a mob saved by StoreMob back to the world at pos, facing yaw, standing still, with home as its home
// (BeehiveBlockActor::_revive). Like BDS it gets a new unique ID. Data without a saved definition (a bee placed by
// world generation, or saved by another server) releases a new mob of the type.
func (BlockHost) ReleaseMob(tx *world.Tx, id string, data map[string]any, pos mgl64.Vec3, yaw float64, home cube.Pos) (world.Entity, bool) {
	t, ok := TypeFor(id)
	if !ok {
		return nil, false
	}
	if _, saved := data["mob:definition"]; !saved {
		m, err := Spawn(tx, id, pos, SpawnOptions{Rotation: cube.Rotation{yaw, 0}})
		if err != nil {
			return nil, false
		}
		m.SetHome(home)
		return m, true
	}
	name, _ := data["NameTag"].(string)
	opts := world.EntitySpawnOpts{Position: pos, Rotation: cube.Rotation{yaw, 0}, NameTag: name}
	m, ok := Of(tx.AddEntity(opts.New(t, restoreConfig{t: t, data: data})))
	if !ok {
		return nil, false
	}
	m.ensureInit()
	m.SetHome(home)
	return m, true
}

// HasNectar reports whether the mob's minecraft:has_nectar property is set.
func (BlockHost) HasNectar(e world.Entity) bool {
	m, ok := Of(e)
	if !ok {
		return false
	}
	v, _ := m.Property(propertyHasNectar)
	b, _ := v.(bool)
	return b
}

// FireEvent fires a definition event on the mob.
func (BlockHost) FireEvent(e world.Entity, event string) {
	if m, ok := Of(e); ok && !m.Dead() {
		m.Fire(event, nil)
	}
}

// restoreConfig makes a mob handle from the data of StoreMob: the mob's NBT is loaded when it initialises.
type restoreConfig struct {
	t    *Type
	data map[string]any
}

// Apply ...
func (c restoreConfig) Apply(d *world.EntityData) {
	c.t.DecodeNBT(c.data, d)
	if v, ok := c.data["Fire"].(int16); ok {
		d.FireDuration = time.Duration(v) * time.Second / 20
	}
	if v, ok := c.data["Age"].(int16); ok {
		d.Age = time.Duration(v) * (time.Second / 20)
	}
}
