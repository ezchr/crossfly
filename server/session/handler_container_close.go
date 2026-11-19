package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// ContainerCloseHandler handles the ContainerClose packet.
type ContainerCloseHandler struct{}

// Handle ...
func (h *ContainerCloseHandler) Handle(p packet.Packet, s *Session, tx *world.Tx, c Controllable) error {
	pk := p.(*packet.ContainerClose)
	trading := s.trade.Load() != nil
	if trading {
		s.conf.Log.Debug("container close while trading", "window", pk.WindowID, "type", pk.ContainerType, "server_side", pk.ServerSide, "open_window", s.openedWindowID.Load())
	}

	c.MoveItemsToInventory()
	s.anvilKeep(tx)

	// A client confirms a close the server made itself (ServerSide, sent by closeWindow or a menu
	// library through writePacket) with a ContainerClose of its own. That one gets no reply: a
	// client takes a reply to it as a response it did not ask for (PacketViolationWarning "Server
	// sent a ContainerClosePacket response packet when it should have sent the server initiated
	// packet").
	confirm := s.serverClosedWindow.CompareAndSwap(int32(pk.WindowID)+1, 0)

	var containerType byte
	switch pk.WindowID {
	case 0:
		// Closing of the normal inventory.
		s.invOpened.Store(false)
	case byte(s.openedWindowID.Load()):
		containerType = byte(s.openedContainerID.Load())
		if !trading {
			s.closeCurrentContainer(tx, true)
		}
	case 0xff:
		// Sent when an inventory/container is opened at the same time as chat.
		s.invOpened.Store(false)
		if !trading {
			if s.containerOpened.Load() {
				s.closeCurrentContainer(tx, false)
			}
			return nil
		}
	default:
		containerType = pk.ContainerType
	}

	if trading {
		// The trading window is the only window the client can have open, so
		// whatever id it closes it under ends the trade. The client waits for
		// an answer carrying its own window id and container type before it
		// opens another window (PowerNukkitX ContainerCloseHandler.sendClose),
		// so echo both back - otherwise the villager never opens again.
		s.closeCurrentContainer(tx, true)
		containerType = pk.ContainerType
	}
	if confirm && !trading {
		return nil
	}
	s.writePacket(&packet.ContainerClose{
		WindowID:      pk.WindowID,
		ContainerType: containerType,
	})
	return nil
}
