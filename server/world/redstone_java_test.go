//go:build crossfly_full

package world

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
)

func TestScheduledTickQueueCancellation(t *testing.T) {
	queue := newScheduledTickQueue(100)
	pos := cube.Pos{8, 64, 8}
	otherPos := pos.Side(cube.FaceEast)
	b := scheduledTickTestBlock{}

	queue.schedule(DefaultBlockRegistry, pos, b, time.Second/20)
	queue.schedule(DefaultBlockRegistry, pos, b, time.Second/10)
	queue.schedule(DefaultBlockRegistry, otherPos, b, time.Second/10)
	queue.cancel(DefaultBlockRegistry, pos, b)

	if ticks := queue.fromChunk(chunkPosFromBlockPos(pos)); len(ticks) != 1 || ticks[0].pos != otherPos {
		t.Fatalf("ticks after cancellation = %v, want only the tick at %v", ticks, otherPos)
	}
	index := scheduledTickIndex{pos: pos, hash: DefaultBlockRegistry.BlockHash(b)}
	if _, ok := queue.furthestTicks[index]; ok {
		t.Fatal("cancelled tick remains in the furthest-tick index")
	}
}
