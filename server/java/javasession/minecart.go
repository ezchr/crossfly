//go:build crossfly_full

package javasession

import (
	"github.com/df-mc/dragonfly/server/entity"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/world"
)

// Entity data of a minecart (VehicleEntity): the hurt time and hurt direction (INT) and damage (FLOAT) the client
// shakes it by. The client draws the block a chest, hopper or TNT minecart carries by itself.
const (
	minecartDataHurt    = 8
	minecartDataHurtDir = 9
	minecartDataDamage  = 10
)

// minecartWobbling is a minecart, which shakes after a hit.
type minecartWobbling interface {
	MinecartWobble() (hurtTime, hurtDir int, damage float32)
}

// minecartWobbleOf returns the shake state of a minecart, if e is one.
func minecartWobbleOf(e world.Entity) (minecartWobbling, bool) {
	if w, ok := e.(minecartWobbling); ok {
		return w, true
	}
	if b, ok := e.(interface{ Behaviour() entity.Behaviour }); ok {
		w, ok := b.Behaviour().(minecartWobbling)
		return w, ok
	}
	return nil, false
}

// viewMinecart sends how a minecart shakes.
func (s *Session) viewMinecart(e world.Entity, id int32) {
	m, ok := minecartWobbleOf(e)
	if !ok {
		return
	}
	hurt, dir, damage := m.MinecartWobble()
	w := s.packet()
	w.VarInt(id)
	w.Byte(minecartDataHurt)
	w.VarInt(dataTypeInt)
	w.VarInt(int32(hurt))
	w.Byte(minecartDataHurtDir)
	w.VarInt(dataTypeInt)
	w.VarInt(int32(dir))
	w.Byte(minecartDataDamage)
	w.VarInt(dataTypeFloat)
	w.Float32(damage)
	w.Byte(0xff)
	s.queue(v777.ClientboundPlaySetEntityData, w)
}

// Entity data of a boat (AbstractBoat): whether the left and right paddles turn (BOOLEAN).
const (
	boatDataPaddleLeft  = 11
	boatDataPaddleRight = 12
)

// viewBoat sends which paddles of a boat turn: the client turns them itself.
func (s *Session) viewBoat(e world.Entity, id int32) {
	b, ok := e.(interface{ BoatPaddles() (left, right bool) })
	if !ok {
		return
	}
	left, right := b.BoatPaddles()
	w := s.packet()
	w.VarInt(id)
	w.Byte(boatDataPaddleLeft)
	w.VarInt(dataTypeBoolean)
	w.Bool(left)
	w.Byte(boatDataPaddleRight)
	w.VarInt(dataTypeBoolean)
	w.Bool(right)
	w.Byte(0xff)
	s.queue(v777.ClientboundPlaySetEntityData, w)
}
