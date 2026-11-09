//go:build crossfly_full

package core

import (
	"encoding/json"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Mob buckets (BDS BucketableComponent, BucketItem). The vanilla fish
// (cod, salmon, pufferfish, tropical fish), the axolotl and the tadpole go
// in water buckets: no behaviour pack file names minecraft:bucketable for
// them, Fish, Axolotl and Tadpole::initializeComponents set it in code, so
// they get it through an interact hook. Definitions may name the key too.

func init() {
	mob.RegisterComponent("minecraft:bucketable", newBucketable)
	mob.RegisterInteractHook(mob.InteractHook{Name: "core/bucketable", Interact: bucketableHook})
	item.SetBucketMobs(bucketMobs{})
}

// bucketable is minecraft:bucketable on a definition (no fields in BDS
// 1.21.3's BucketableDescription).
type bucketable struct{}

func newBucketable(string, json.RawMessage) (mob.Component, error) { return bucketable{}, nil }

func (bucketable) Attach(*mob.Mob) {}
func (bucketable) Detach(*mob.Mob) {}

// Interact implements mob.Interactor.
func (bucketable) Interact(m *mob.Mob, user item.User, held item.Stack, _ mgl64.Vec3) (item.Stack, bool) {
	return BucketMob(m, user, held)
}

// bucketableHook is the BucketableDescription Fish, Axolotl and Tadpole set
// in code, for mobs whose definition does not name minecraft:bucketable.
func bucketableHook(m *mob.Mob, user item.User, held item.Stack, _ mgl64.Vec3) (item.Stack, bool) {
	if m.HasComponent("minecraft:bucketable") {
		return held, false
	}
	return BucketMob(m, user, held)
}

// BucketMob puts m in the water bucket the user holds, as BDS
// BucketableComponent::getInteraction/implInteraction with
// BucketItem::_useOn: only a plain water bucket (data 0) takes a mob, and
// only a mob with a bucket item. The mob's save data goes into the bucket
// (addBucketEntitySaveData), bucket.fill.fish plays, the mob is removed and
// the held bucket becomes the mob bucket (also in creative mode, as the
// bucket carries data; a stack of more than one gives it to the inventory).
func BucketMob(m *mob.Mob, user item.User, held item.Stack) (item.Stack, bool) {
	if held.Empty() || m.Dead() {
		return held, false
	}
	if name, meta := held.Item().EncodeItem(); name != "minecraft:water_bucket" || meta != 0 {
		return held, false
	}
	b, ok := item.NewMobBucket(m.Identifier(), bucketSaveData(m))
	if !ok {
		return held, false
	}
	m.Tx().PlaySound(m.Position(), item.MobBucketSound{Event: "bucket.fill.fish", Mob: m.Identifier()})
	left := ReplaceOne(m, user, held, item.NewStack(b, 1))
	_ = m.Close()
	return left, true
}

// bucketSaveData is BDS BucketItem::addBucketEntitySaveData: the mob's save
// data (with the Variant, MarkVariant, Color and Color2 BDS actors save)
// and the keys the client names the bucket from: a named mob's CustomName,
// else the axolotl's colour and body, else the tropical fish's school
// (GroupName) or colours and body. AppendCustomName is set with any of them.
func bucketSaveData(m *mob.Mob) map[string]any {
	d := m.SaveData()
	meta := m.Meta()
	d["Variant"], d["MarkVariant"] = meta.Variant, meta.MarkVariant
	d["Color"], d["Color2"] = uint8(meta.Color), uint8(meta.Color2)
	switch {
	case m.NameTag() != "":
		d["CustomName"], d["AppendCustomName"] = m.NameTag(), uint8(1)
	case m.Identifier() == "minecraft:axolotl":
		if v := int(meta.Variant); v >= 0 && v < len(axolotlColourIDs) {
			d["ColorID"] = axolotlColourIDs[v]
		}
		d["BodyID"] = "item.axolotlAdultBodySingle.name"
		if IsBaby(m) {
			d["BodyID"] = "item.axolotlBabyBodySingle.name"
		}
		d["AppendCustomName"] = uint8(1)
	case m.Identifier() == "minecraft:tropicalfish":
		v, mark, c, c2 := meta.Variant, meta.MarkVariant, int(meta.Color), int(meta.Color2)
		if name, ok := tropicalSchool(c, c2, v, mark); ok {
			d["GroupName"] = name
		} else {
			if c >= 0 && c < len(tropicalColourIDs) {
				d["ColorID"] = tropicalColourIDs[c]
			}
			if c != c2 && c2 >= 0 && c2 < len(tropicalColourIDs) {
				d["Color2ID"] = tropicalColourIDs[c2]
			}
			body := int(mark)
			if v == 1 {
				body += len(tropicalBodyIDs) / 4
			}
			if c != c2 {
				body += len(tropicalBodyIDs) / 2
			}
			if body >= 0 && body < len(tropicalBodyIDs) {
				d["BodyID"] = tropicalBodyIDs[body]
			}
		}
		d["AppendCustomName"] = uint8(1)
	}
	return d
}

// axolotlColourIDs are BDS's axolotl colour names by variant.
var axolotlColourIDs = [...]string{"item.axolotlColorLucy.name", "item.axolotlColorCyan.name",
	"item.axolotlColorGold.name", "item.axolotlColorWild.name", "item.axolotlColorBlue.name"}

// tropicalColourIDs are BDS's tropical fish colour names by colour.
var tropicalColourIDs = [...]string{"item.tropicalColorWhite.name", "item.tropicalColorOrange.name",
	"item.tropicalColorMagenta.name", "item.tropicalColorSky.name", "item.tropicalColorYellow.name",
	"item.tropicalColorLime.name", "item.tropicalColorRose.name", "item.tropicalColorGray.name",
	"item.tropicalColorSilver.name", "item.tropicalColorTeal.name", "item.tropicalColorPlum.name",
	"item.tropicalColorBlue.name", "item.tropicalColorBrown.name", "item.tropicalColorGreen.name",
	"item.tropicalColorRed.name"}

// tropicalBodyIDs are BDS's tropical fish body names: the small (variant 0)
// then large (variant 1) patterns by mark variant, in one colour, then the
// same in two colours.
var tropicalBodyIDs = [...]string{
	"item.tropicalBodyKobSingle.name", "item.tropicalBodySunstreakSingle.name", "item.tropicalBodySnooperSingle.name",
	"item.tropicalBodyDasherSingle.name", "item.tropicalBodyBrinelySingle.name", "item.tropicalBodySpottySingle.name",
	"item.tropicalBodyFlopperSingle.name", "item.tropicalBodyStripeySingle.name", "item.tropicalBodyGlitterSingle.name",
	"item.tropicalBodyBlockfishSingle.name", "item.tropicalBodyBettySingle.name", "item.tropicalBodyClayfishSingle.name",
	"item.tropicalBodyKobMulti.name", "item.tropicalBodySunstreakMulti.name", "item.tropicalBodySnooperMulti.name",
	"item.tropicalBodyDasherMulti.name", "item.tropicalBodyBrinelyMulti.name", "item.tropicalBodySpottyMulti.name",
	"item.tropicalBodyFlopperMulti.name", "item.tropicalBodyStripeyMulti.name", "item.tropicalBodyGlitterMulti.name",
	"item.tropicalBodyBlockfishMulti.name", "item.tropicalBodyBettyMulti.name", "item.tropicalBodyClayfishMulti.name",
}

// tropicalSchools are BDS's named tropical fish (colour, colour 2, variant,
// mark variant) in BDS's order; the values are the tropicalfish.json groups
// of the same fish.
var tropicalSchools = [...]struct {
	c, c2, v, mark int32
	name           string
}{
	{1, 7, 1, 1, "item.tropicalSchoolAnemone.name"},
	{7, 7, 1, 0, "item.tropicalSchoolBlackTang.name"},
	{7, 3, 0, 1, "item.tropicalSchoolBlueDory.name"},
	{0, 7, 0, 4, "item.tropicalSchoolButterflyFish.name"},
	{11, 7, 0, 1, "item.tropicalSchoolCichlid.name"},
	{1, 0, 0, 0, "item.tropicalSchoolClownfish.name"},
	{6, 3, 0, 5, "item.tropicalSchoolCottonCandyBetta.name"},
	{10, 4, 1, 3, "item.tropicalSchoolDottyback.name"},
	{0, 14, 1, 5, "item.tropicalSchoolEmperorRedSnapper.name"},
	{0, 4, 0, 5, "item.tropicalSchoolGoatfish.name"},
	{0, 7, 1, 2, "item.tropicalSchoolMoorishIdol.name"},
	{0, 1, 1, 5, "item.tropicalSchoolOrnateButterfly.name"},
	{9, 6, 0, 3, "item.tropicalSchoolParrotfish.name"},
	{5, 3, 0, 4, "item.tropicalSchoolQueenAngelFish.name"},
	{14, 0, 1, 4, "item.tropicalSchoolRedCichlid.name"},
	{7, 14, 0, 2, "item.tropicalSchoolRedLippedBlenny.name"},
	{14, 0, 1, 3, "item.tropicalSchoolRedSnapper.name"},
	{0, 4, 1, 0, "item.tropicalSchoolThreadfin.name"},
	{14, 0, 0, 1, "item.tropicalSchoolTomatoClown.name"},
	{7, 0, 0, 1, "item.tropicalSchoolTriggerfish.name"},
	{9, 4, 0, 3, "item.tropicalSchoolYellowtailParrot.name"},
	{4, 4, 1, 1, "item.tropicalSchoolYellowTang.name"},
}

// tropicalSchool returns the name of the named tropical fish with these
// colours and pattern, if it is one.
func tropicalSchool(c, c2 int, v, mark int32) (string, bool) {
	for _, s := range tropicalSchools {
		if int(s.c) == c && int(s.c2) == c2 && s.v == v && s.mark == mark {
			return s.name, true
		}
	}
	return "", false
}

// bucketMobs implements item.BucketMobs.
type bucketMobs struct{}

// ReleaseFromBucket is BDS BucketItem::readBucketEntitySaveData: the mob is
// loaded from the bucket's data (a new one is spawned, firing
// entity_spawned, when there is none or it does not load) in the middle of
// the block at its bottom, and made persistent.
func (bucketMobs) ReleaseFromBucket(tx *world.Tx, id string, data map[string]any, pos cube.Pos) bool {
	at := mgl64.Vec3{float64(pos[0]) + 0.5, float64(pos[1]), float64(pos[2]) + 0.5}
	var m *mob.Mob
	if data != nil {
		if s, _ := data["identifier"].(string); s == id {
			m, _ = mob.SpawnSaved(tx, data, at)
		}
	}
	if m == nil {
		var err error
		if m, err = mob.Spawn(tx, id, at, mob.SpawnOptions{}); err != nil {
			return false
		}
	}
	m.SetPersistent(true)
	return true
}
