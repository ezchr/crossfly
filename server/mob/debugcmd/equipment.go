//go:build crossfly_full

package debugcmd

import (
	"fmt"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/component/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type (
	// equipment is /mobdebug equipment: the slots of the looked-at mob with
	// their drop chances.
	equipment struct {
		gate
		Equipment cmd.SubCommand `cmd:"equipment"`
	}
	// gear is /mobdebug gear [count]: spawns count (default 1) of each
	// mob that has spawn equipment in a row in front of the player and
	// prints what each got.
	gear struct {
		gate
		Gear  cmd.SubCommand    `cmd:"gear"`
		Count cmd.Optional[int] `cmd:"count"`
	}
)

// GearMobs are the mobs /mobdebug gear spawns: every vanilla mob with a
// minecraft:equipment table.
var GearMobs = []string{"minecraft:zombie", "minecraft:husk", "minecraft:drowned", "minecraft:skeleton",
	"minecraft:stray", "minecraft:bogged", "minecraft:wither_skeleton", "minecraft:pillager", "minecraft:vindicator",
	"minecraft:piglin", "minecraft:piglin_brute", "minecraft:zombie_pigman", "minecraft:vex", "minecraft:fox"}

// Run ...
func (equipment) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	if m, ok := target(src, o, tx); ok {
		printLines(o, EquipmentString(m))
	}
}

// Run ...
func (g gear) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	if tx == nil {
		o.Error("/mobdebug must be run in a world")
		return
	}
	v, ok := src.(viewer)
	if !ok {
		o.Error("/mobdebug must be run by a player")
		return
	}
	n := min(max(g.Count.LoadOr(1), 1), 10)
	yaw := v.Rotation().Yaw() * math.Pi / 180
	fwd := mgl64.Vec3{-math.Sin(yaw), 0, math.Cos(yaw)}
	side := mgl64.Vec3{fwd[2], 0, -fwd[0]}
	for i, id := range GearMobs {
		var lines []string
		for j := 0; j < n; j++ {
			pos := v.Position().Add(fwd.Mul(4 + float64(j)*1.5)).Add(side.Mul(float64(i-len(GearMobs)/2) * 1.5))
			m, err := mob.Spawn(tx, id, pos, mob.SpawnOptions{})
			if err != nil {
				o.Errorf("%v: %v", entityName(id), err)
				break
			}
			core.RollEquipment(m)
			lines = append(lines, gearLine(m))
		}
		o.Printf("%v: %v", entityName(id), strings.Join(lines, " | "))
	}
}

// EquipmentString lists m's equipment slots: item, enchantments, drop
// chance, picked-up mark.
func EquipmentString(m *mob.Mob) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%v equipment:\n", entityName(m.Identifier()))
	for slot, n := range core.SlotNames {
		s := core.EquipmentSlot(m, slot)
		fmt.Fprintf(&b, "  %v: %v (drop %.1f%%", strings.TrimPrefix(n, "slot."), stackString(s), core.DropChance(m, slot)*100)
		if loot.WasPickedUp(s) {
			b.WriteString(", picked up: always drops")
		}
		b.WriteString(")\n")
	}
	if body := m.BodyArmour(); !body.Empty() {
		fmt.Fprintf(&b, "  armor.body: %v\n", stackString(body))
	}
	return b.String()
}

func gearLine(m *mob.Mob) string {
	var parts []string
	for slot := range core.SlotNames {
		if s := core.EquipmentSlot(m, slot); !s.Empty() {
			parts = append(parts, stackString(s))
		}
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}

func stackString(s item.Stack) string {
	if s.Empty() {
		return "empty"
	}
	n, _ := s.Item().EncodeItem()
	str := strings.TrimPrefix(n, "minecraft:")
	if s.Count() > 1 {
		str = fmt.Sprintf("%dx %v", s.Count(), str)
	}
	var es []string
	for _, e := range s.Enchantments() {
		es = append(es, fmt.Sprintf("%v %d", e.Type().Name(), e.Level()))
	}
	if len(es) > 0 {
		str += " [" + strings.Join(es, ", ") + "]"
	}
	if md := s.MaxDurability(); md > 0 && s.Durability() < md {
		str += fmt.Sprintf(" %d/%d", s.Durability(), md)
	}
	return str
}
