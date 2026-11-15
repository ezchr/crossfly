package player

import (
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Break log (DF_BREAK_LOG=1): for every block a player finishes breaking, how long it really took against
// the break time the server worked out (which sets the crack animation speed), and the inputs of that break
// time: on ground, flying, in water, the held item. For finding why cracks lag behind the real breaking.

var breakLog = os.Getenv("DF_BREAK_LOG") == "1"

type breakRec struct {
	start    time.Time
	expected time.Duration
	changes  []string
}

var breakRecs sync.Map // uuid.UUID -> *breakRec

func (p *Player) breakState() string {
	held, _ := p.HeldItems()
	name := "hand"
	if !held.Empty() {
		n, _ := held.Item().EncodeItem()
		name = n
	}
	ctx := p.breakContext()
	return fmt.Sprintf("onGround=%v airborne=%v flying=%v underwater=%v haste=%d fatigue=%d held=%s ench=%v",
		p.OnGround(), ctx.Airborne, ctx.Flying, ctx.Underwater, ctx.HasteLevel, ctx.MiningFatigueLevel, name, held.Enchantments())
}

func (p *Player) logBreakStart(expected time.Duration) {
	if !breakLog {
		return
	}
	breakRecs.Store(p.UUID(), &breakRec{start: time.Now(), expected: expected})
	slog.Info("break start", "player", p.Name(), "block", fmt.Sprintf("%T", p.tx.Block(p.breakingPos)), "expected", expected, "state", p.breakState())
}

func (p *Player) logBreakChange(t time.Duration) {
	if !breakLog {
		return
	}
	if v, ok := breakRecs.Load(p.UUID()); ok {
		r := v.(*breakRec)
		r.changes = append(r.changes, fmt.Sprintf("%v@%.2fs(%s)", t, time.Since(r.start).Seconds(), p.breakState()))
	}
}

func (p *Player) logBreakFinish() {
	if !breakLog {
		return
	}
	v, ok := breakRecs.LoadAndDelete(p.UUID())
	if !ok {
		return
	}
	r := v.(*breakRec)
	took := time.Since(r.start)
	ratio := 0.0
	if p.lastBreakDuration > 0 {
		ratio = took.Seconds() / p.lastBreakDuration.Seconds()
	}
	slog.Info("break finish", "player", p.Name(), "took", took.Round(time.Millisecond), "expectedAtStart", r.expected,
		"expectedNow", p.lastBreakDuration, "crackShownAtBreak", fmt.Sprintf("%.0f%%", 100*min(ratio, 1)),
		"changes", r.changes, "state", p.breakState())
}

var _ = uuid.UUID{}
