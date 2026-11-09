package javasession

import (
	"sync"

	"github.com/google/uuid"

	"github.com/df-mc/dragonfly/server/java/protocol/text"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/world"
)

// Entities drawn for Java players as a text display: the entity's Java stand-in
// (RegisterEntityAlias) is a minecraft:text_display, shown with the text and look its TextModel
// gives. A text display is drawn without the light from the side an item display gets, so a
// picture drawn as one glyph of a font of the server's Java resource pack keeps its colours from
// every angle.

// TextModel is how a Java player sees an entity whose stand-in is a text display.
type TextModel struct {
	Text text.Component
	// Scale is how much bigger the text is drawn than a text display draws it, on top of the
	// entity's own Scale; 0 is 1.
	Scale float64
	// Billboard turns the text to face the viewer (BillboardCenter, BillboardVertical); 0: it
	// turns with the entity.
	Billboard byte
	// FullBright draws the text at full light, whatever light is around it.
	FullBright bool
	// Background is the colour of the box behind the text (ARGB); 0 is none.
	Background int32
	// Back, if set, is shown from behind (a text display is drawn from the front only): a second
	// text display turned half round. For a model without a Billboard.
	Back *text.Component
}

// Billboard modes of a TextModel: BillboardCenter faces the viewer fully, BillboardVertical turns
// about the vertical axis only.
const (
	BillboardVertical = 1
	BillboardCenter   = 3
)

// Entity data of a text display's background colour (TextDisplay.DATA_BACKGROUND_COLOR_ID).
const dataTextBackground = 25

var (
	textModelsMu sync.RWMutex
	textModels   = map[string]func(world.Entity) *TextModel{}
)

// RegisterTextModel draws the entities with the Bedrock identifier bedrock as the text display
// pick returns for each (nil: the stand-in as it is). Their stand-in must be minecraft:text_display.
func RegisterTextModel(bedrock string, pick func(world.Entity) *TextModel) {
	textModelsMu.Lock()
	defer textModelsMu.Unlock()
	textModels[bedrock] = pick
}

func textModelFor(e world.Entity) *TextModel {
	textModelsMu.RLock()
	pick := textModels[e.H().Type().EncodeEntity()]
	textModelsMu.RUnlock()
	if pick == nil {
		return nil
	}
	return pick(e)
}

// viewTextModel sends the text and look of e (Java id id), drawn as the text display m, with a
// second text display turned half round showing m.Back (a part of its model, so it goes with it).
func (s *Session) viewTextModel(e world.Entity, id int32, m *TextModel) {
	s.writeTextModel(e, id, m)
	if m.Back == nil || m.Billboard != 0 {
		return
	}
	typ := s.ver.Builtin("minecraft:entity_type", v777.BuiltinID("minecraft:entity_type", "minecraft:text_display"))
	if typ < 0 {
		return
	}
	back := s.clientEntityID()
	pos, yaw := e.Position(), e.Rotation().Yaw()+180
	w := s.packet()
	w.VarInt(back)
	w.UUID(uuid.New())
	w.VarInt(typ)
	w.Float64(pos[0])
	w.Float64(pos[1])
	w.Float64(pos[2])
	w.LpVec3(0, 0, 0)
	w.Angle(0)
	w.Angle(float32(yaw))
	w.Angle(float32(yaw))
	w.VarInt(0)
	s.queue(v777.ClientboundPlayAddEntity, w)
	bm := *m
	bm.Text, bm.Back = *m.Back, nil
	s.writeTextModel(e, back, &bm)
	s.entMu.Lock()
	s.modelParts[id] = append(s.modelParts[id], back)
	s.entMu.Unlock()
}

// writeTextModel sends the text and look of the text display m to the Java entity id.
func (s *Session) writeTextModel(e world.Entity, id int32, m *TextModel) {
	f := float32(1)
	if m.Scale > 0 {
		f = float32(m.Scale)
	}
	if sc, ok := e.(interface{ Scale() float64 }); ok && sc.Scale() > 0 {
		f *= float32(sc.Scale())
	}
	w := s.packet()
	w.VarInt(id)
	if m.Billboard != 0 {
		w.Byte(dataDisplayBillboard)
		w.VarInt(dataTypeByte)
		w.Byte(m.Billboard)
	}
	if m.FullBright {
		w.Byte(dataDisplayBrightness)
		w.VarInt(dataTypeInt)
		w.VarInt(fullBrightness)
	}
	if f != 1 {
		w.Byte(dataDisplayScale)
		w.VarInt(dataTypeVector3)
		w.Float32(f)
		w.Float32(f)
		w.Float32(f)
	}
	w.Byte(dataTextLineWidth)
	w.VarInt(dataTypeInt)
	w.VarInt(1 << 16) // never wrapped
	w.Byte(dataTextBackground)
	w.VarInt(dataTypeInt)
	w.VarInt(m.Background)
	w.Byte(dataTextText)
	w.VarInt(dataTypeComponent)
	m.Text.Write(w)
	w.Byte(0xff)
	s.queue(v777.ClientboundPlaySetEntityData, w)

	s.entMu.Lock()
	if s.textModelIDs == nil {
		s.textModelIDs = map[int32]struct{}{}
	}
	s.textModelIDs[id] = struct{}{}
	s.entMu.Unlock()
}

// remeasureTextModels makes the client measure the text of every text model it shows again. A
// text display centres its text by the width it measured when the text arrived, and keeps it: a
// picture shown before the resource pack with its font was loaded (packs a proxy offers come
// after the player is in the world) was measured as nothing and hung off to one side for good. A
// new line width (one less every other time) makes the client measure the text again.
func (s *Session) remeasureTextModels() {
	s.entMu.Lock()
	ids := make([]int32, 0, len(s.textModelIDs))
	for id := range s.textModelIDs {
		ids = append(ids, id)
	}
	s.remeasure = !s.remeasure
	width := int32(1 << 16)
	if s.remeasure {
		width--
	}
	s.entMu.Unlock()
	for _, id := range ids {
		w := s.packet()
		w.VarInt(id)
		w.Byte(dataTextLineWidth)
		w.VarInt(dataTypeInt)
		w.VarInt(width)
		w.Byte(0xff)
		s.queue(v777.ClientboundPlaySetEntityData, w)
	}
}
