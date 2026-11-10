package block

import (
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// Jukebox is a block used to play music discs.
type Jukebox struct {
	solid
	bass

	// Item is the music disc played by the jukebox.
	Item item.Stack

	// handle is the sound handle of the music disc currently playing.
	handle uint64
	// started is the world tick the music disc started playing at.
	started int64
}

// JukeboxListener is the optional part of the MobHost that hears jukeboxes: allays and parrots
// dance near a playing jukebox (BDS DanceComponentListener, which hears the record level sounds of
// JukeboxBlockActor).
type JukeboxListener interface {
	// JukeboxPlaying is called with playing true when a disc starts and once a second while it plays
	// (the record.playing sound of JukeboxBlockActor::tick), and with false when the music stops: the
	// disc is taken out, the jukebox is broken or the song is over (record.null).
	JukeboxPlaying(tx *world.Tx, pos cube.Pos, playing bool)
}

// jukeboxPlaying tells the mobs about the jukebox at pos.
func jukeboxPlaying(tx *world.Tx, pos cube.Pos, playing bool) {
	if l, ok := Mobs().(JukeboxListener); ok {
		l.JukeboxPlaying(tx, pos, playing)
	}
}

// discSeconds is how long each music disc plays (the duration of RecordItemComponent, set by
// RecordItem::RecordItem in BDS 1.21.3; tears and lava chicken from the 26.60 server).
var discSeconds = map[sound.DiscType]float64{
	sound.Disc13(): 178, sound.DiscCat(): 185, sound.DiscBlocks(): 345, sound.DiscChirp(): 185,
	sound.DiscFar(): 174, sound.DiscMall(): 197, sound.DiscMellohi(): 96, sound.DiscStal(): 150,
	sound.DiscStrad(): 188, sound.DiscWard(): 251, sound.Disc11(): 71, sound.DiscWait(): 238,
	sound.DiscOtherside(): 195, sound.DiscPigstep(): 149, sound.Disc5(): 178, sound.DiscRelic(): 218,
	sound.DiscCreator(): 176, sound.DiscCreatorMusicBox(): 73, sound.DiscPrecipice(): 299,
	sound.DiscTears(): 175, sound.DiscLavaChicken(): 134,
}

// startDisc puts the disc s in the jukebox at pos and starts the music.
func (j Jukebox) startDisc(pos cube.Pos, tx *world.Tx, s item.Stack, d sound.DiscType) {
	j.Item, j.handle, j.started = s, tx.World().NextSoundHandle(), tx.CurrentTick()
	tx.SetBlock(pos, j, nil)
	tx.PlaySound(pos.Vec3Centre(), sound.MusicDiscPlay{DiscType: d, Handle: j.handle})
	jukeboxPlaying(tx, pos, true)
	tx.ScheduleBlockUpdate(pos, j, time.Second)
}

// ScheduledTick runs once a second while a disc plays (JukeboxBlockActor::tick): the mobs hear the
// music again until the song is over, when the music stops as if the disc had been taken out.
func (j Jukebox) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	d, ok := j.Disc()
	elapsed := tx.CurrentTick() - j.started
	if !ok || j.handle == 0 || elapsed%20 != 0 {
		// No disc, or a tick still scheduled for a disc taken out since.
		return
	}
	// JukeboxBlockActor::_spawnMusicParticles: over once TicksPlaying*0.05 passes the duration.
	if float64(elapsed)*0.05 > discSeconds[d] {
		tx.PlaySound(pos.Vec3Centre(), sound.MusicDiscEnd{Handle: j.handle})
		jukeboxPlaying(tx, pos, false)
		return
	}
	jukeboxPlaying(tx, pos, true)
	tx.ScheduleBlockUpdate(pos, j, time.Second)
}

// InsertItem ...
func (j Jukebox) InsertItem(h Hopper, pos cube.Pos, tx *world.Tx) bool {
	if !j.Item.Empty() {
		return false
	}

	for sourceSlot, sourceStack := range h.inventory.Slots() {
		if sourceStack.Empty() {
			continue
		}

		if m, ok := sourceStack.Item().(item.MusicDisc); ok {
			_ = h.inventory.SetItem(sourceSlot, sourceStack.Grow(-1))
			j.startDisc(pos, tx, sourceStack, m.DiscType)
			return true
		}
	}

	return false
}

// ExtractItem ...
func (j Jukebox) ExtractItem(_ Hopper, _ cube.Pos, _ *world.Tx) bool {
	// TODO: This functionality requires redstone to be implemented.
	return false
}

// FuelInfo ...
func (j Jukebox) FuelInfo() item.FuelInfo {
	return newFuelInfo(time.Second * 15)
}

// BreakInfo ...
func (j Jukebox) BreakInfo() BreakInfo {
	return newBreakInfo(2, alwaysHarvestable, axeEffective, oneOf(Jukebox{})).withBlastResistance(6).withBreakHandler(func(pos cube.Pos, tx *world.Tx, u item.User) {
		if _, hasDisc := j.Disc(); hasDisc {
			dropItem(tx, j.Item, pos.Vec3())
			tx.PlaySound(pos.Vec3Centre(), sound.MusicDiscEnd{Handle: j.handle})
			jukeboxPlaying(tx, pos, false)
		}
	})
}

// jukeboxUser represents an item.User that can use a jukebox.
type jukeboxUser interface {
	item.User
	// SendJukeboxPopup sends a jukebox popup to the item.User.
	SendJukeboxPopup(a ...any)
}

// Activate ...
func (j Jukebox) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	if _, hasDisc := j.Disc(); hasDisc {
		dropItem(tx, j.Item, pos.Side(cube.FaceUp).Vec3Middle())

		tx.PlaySound(pos.Vec3Centre(), sound.MusicDiscEnd{Handle: j.handle})
		j.Item, j.handle = item.Stack{}, 0
		tx.SetBlock(pos, j, nil)
		jukeboxPlaying(tx, pos, false)
	} else if held, _ := u.HeldItems(); !held.Empty() {
		if m, ok := held.Item().(item.MusicDisc); ok {
			ctx.SubtractFromCount(1)
			j.startDisc(pos, tx, held, m.DiscType)
			if u, ok := u.(jukeboxUser); ok {
				u.SendJukeboxPopup(fmt.Sprintf("Now playing: %v - %v", m.DiscType.Author(), m.DiscType.DisplayName()))
			}
		}
	}
	return true
}

// Disc returns the currently playing music disc
func (j Jukebox) Disc() (sound.DiscType, bool) {
	if !j.Item.Empty() {
		if m, ok := j.Item.Item().(item.MusicDisc); ok {
			return m.DiscType, true
		}
	}
	return sound.DiscType{}, false
}

// EncodeNBT ...
func (j Jukebox) EncodeNBT() map[string]any {
	m := map[string]any{"id": "Jukebox"}
	if _, hasDisc := j.Disc(); hasDisc {
		m["RecordItem"] = item.WriteNBT(j.Item, true)
	}
	return m
}

// DecodeNBT ...
func (j Jukebox) DecodeNBT(data map[string]any) any {
	s := item.MapNBT(data, "RecordItem")
	if _, ok := s.Item().(item.MusicDisc); ok {
		j.Item = s
	}
	return j
}

// EncodeItem ...
func (Jukebox) EncodeItem() (name string, meta int16) {
	return "minecraft:jukebox", 0
}

// EncodeBlock ...
func (Jukebox) EncodeBlock() (string, map[string]any) {
	return "minecraft:jukebox", nil
}
