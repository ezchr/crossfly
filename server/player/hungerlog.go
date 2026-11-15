package player

import (
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Hunger log (DF_HUNGER_LOG=1): every 10 seconds, per player, the exhaustion taken and where it came from
// (the calling line of Exhaust), with the sprint state, food and saturation. For finding why players
// starve faster than in vanilla. Off by default: Exhaust then costs one bool check.

var hungerLog = os.Getenv("DF_HUNGER_LOG") == "1"

type hungerStat struct {
	mu      sync.Mutex
	start   time.Time
	bySrc   map[string]float64
	calls   map[string]int
	toggles []string
}

var hungerStats sync.Map // uuid.UUID -> *hungerStat

func hungerStatFor(id uuid.UUID) *hungerStat {
	v, _ := hungerStats.LoadOrStore(id, &hungerStat{start: time.Now(), bySrc: map[string]float64{}, calls: map[string]int{}})
	return v.(*hungerStat)
}

// logExhaust records points taken by Exhaust, labelled by Exhaust's caller.
func (p *Player) logExhaust(points float64) {
	src := "?"
	if _, file, line, ok := runtime.Caller(2); ok {
		src = fmt.Sprintf("%s:%d", file[strings.LastIndex(file, "/")+1:], line)
	}
	st := hungerStatFor(p.UUID())
	st.mu.Lock()
	st.bySrc[src] += points
	st.calls[src]++
	st.mu.Unlock()
	p.flushHungerLog(st, false)
}

// logSprintToggle records a sprint start or stop the server applied.
func (p *Player) logSprintToggle(on bool) {
	if !hungerLog {
		return
	}
	st := hungerStatFor(p.UUID())
	st.mu.Lock()
	st.toggles = append(st.toggles, fmt.Sprintf("%s@%.1fs", map[bool]string{true: "start", false: "stop"}[on], time.Since(st.start).Seconds()))
	st.mu.Unlock()
}

func (p *Player) flushHungerLog(st *hungerStat, force bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	el := time.Since(st.start)
	if !force && el < 10*time.Second {
		return
	}
	var total float64
	srcs := make([]string, 0, len(st.bySrc))
	for s, v := range st.bySrc {
		total += v
		srcs = append(srcs, s)
	}
	sort.Slice(srcs, func(i, j int) bool { return st.bySrc[srcs[i]] > st.bySrc[srcs[j]] })
	parts := make([]string, 0, len(srcs))
	for _, s := range srcs {
		parts = append(parts, fmt.Sprintf("%s=%.2f(x%d)", s, st.bySrc[s], st.calls[s]))
	}
	slog.Info("hunger", "player", p.Name(), "secs", fmt.Sprintf("%.1f", el.Seconds()), "exhaustion", fmt.Sprintf("%.2f", total),
		"food", p.hunger.Food(), "saturation", fmt.Sprintf("%.2f", p.hungerSaturation()), "sprinting", p.sprinting,
		"onGround", p.onGround, "sources", strings.Join(parts, " "), "sprintToggles", strings.Join(st.toggles, ","))
	st.start, st.bySrc, st.calls, st.toggles = time.Now(), map[string]float64{}, map[string]int{}, nil
}

// hungerSaturation reads the saturation level under the hunger lock.
func (p *Player) hungerSaturation() float64 {
	p.hunger.mu.RLock()
	defer p.hunger.mu.RUnlock()
	return p.hunger.saturationLevel
}
