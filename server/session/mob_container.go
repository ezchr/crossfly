package session

import (
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// Entity inventory windows (a horse's saddle, armour and chest; entity.WindowOf). As Geyser opens
// the mount screen: UpdateEquip with the horse window type, the entity's unique ID and the
// accepted items of the equipment slots, then the window's InventoryContent. The client then
// moves items with ItemStackRequests on container HORSE_EQUIP (equipment slot n) and LEVEL_ENTITY
// (window slot n), both the window's index n (invByID).

// entityWindows is the open entity window of each session.
var entityWindows sync.Map // *Session -> *entityWindowState

type entityWindowState struct {
	entity  *world.EntityHandle
	onClose func()
}

// OpenEntityContainer opens the inventory window of e, if it has one the player may open, and
// reports whether it did (player.OpenEntityContainer).
func (s *Session) OpenEntityContainer(e world.Entity, tx *world.Tx) bool {
	var viewer world.Entity
	if s.ent != nil {
		viewer, _ = s.ent.Entity(tx)
	}
	if viewer == nil {
		return false
	}
	w, ok := entity.WindowOf(e, viewer, entity.WindowLayoutBedrock)
	if !ok || w.Inventory == nil {
		return false
	}
	s.closeCurrentContainer(tx, false)

	id := s.nextWindowID()
	pos := cube.PosFromVec3(e.Position())
	s.containerOpened.Store(true)
	s.openedWindow.Store(w.Inventory)
	s.openedPos.Store(&pos)
	entityWindows.Store(s, &entityWindowState{entity: e.H(), onClose: w.OnClose})
	if w.Chest {
		// A chest boat: ContainerOpen with the boat's unique ID, its slots on container LEVEL_ENTITY, as BDS opens
		// minecraft:inventory of type chest_boat.
		s.openedContainerID.Store(uint32(protocol.ContainerTypeChestBoat))
		s.writePacket(&packet.ContainerOpen{
			WindowID:                id,
			ContainerType:           protocol.ContainerTypeChestBoat,
			ContainerPosition:       protocol.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])},
			ContainerEntityUniqueID: int64(s.entityRuntimeID(e)),
		})
		s.sendInv(w.Inventory, uint32(id))
		return true
	}
	s.openedContainerID.Store(uint32(protocol.ContainerTypeHorse))

	slots := make([]any, 0, len(w.Equip))
	for _, eq := range w.Equip {
		accepted := make([]any, 0, len(eq.Accepted))
		for _, name := range eq.Accepted {
			accepted = append(accepted, map[string]any{"slotItem": map[string]any{"Aux": int16(0x7fff), "Name": itemName(name)}})
		}
		icon := eq.Icon
		if icon == "" && len(eq.Accepted) > 0 {
			icon = eq.Accepted[0]
		}
		slots = append(slots, map[string]any{
			"acceptedItems": accepted,
			"item":          map[string]any{"Aux": int16(0x7fff), "Name": itemName(icon)},
			"slotNumber":    int32(eq.Slot),
		})
	}
	data, err := nbt.MarshalEncoding(map[string]any{"slots": slots}, nbt.NetworkLittleEndian)
	if err != nil {
		s.conf.Log.Debug("open entity window: encode equipment", "err", err)
	}
	s.writePacket(&packet.UpdateEquip{
		WindowID:                id,
		WindowType:              protocol.ContainerTypeHorse,
		EntityUniqueID:          int64(s.entityRuntimeID(e)),
		SerialisedInventoryData: data,
	})
	s.sendInv(w.Inventory, uint32(id))
	return true
}

// itemName adds the minecraft: namespace to an item name of the entity files ("saddle").
func itemName(name string) string {
	if name == "" || strings.Contains(name, ":") {
		return name
	}
	return "minecraft:" + name
}

// entityWindowOpen reports whether the session has an entity window open.
func (s *Session) entityWindowOpen() bool {
	_, ok := entityWindows.Load(s)
	return ok && s.containerOpened.Load()
}

// closeEntityWindow closes an open entity window and reports whether there was one.
func (s *Session) closeEntityWindow(clientRequested bool) bool {
	v, ok := entityWindows.LoadAndDelete(s)
	if !ok {
		return false
	}
	s.closeWindow(clientRequested)
	if st := v.(*entityWindowState); st.onClose != nil {
		st.onClose()
	}
	return true
}

// openVehicleInventory opens the window of the vehicle c rides (the inventory key while riding a
// horse) and reports whether it did.
func (s *Session) openVehicleInventory(tx *world.Tx, c Controllable) bool {
	rideable, ok := c.RidingEntity(tx)
	return ok && s.OpenEntityContainer(rideable, tx)
}

// drivenBySelf reports whether e is a vehicle this session's client moves itself (a horse it
// predicts): its movement is not sent back to it.
func (s *Session) drivenBySelf(e world.Entity) bool {
	return entity.DrivenBy(e, s.ent)
}
