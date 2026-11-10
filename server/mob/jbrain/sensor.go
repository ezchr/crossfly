package jbrain

import "sync"

// Sensor is the body of a Java sensor (Sensor.doTick and Sensor.requires).
// Sensors keep per-brain state: SensorType.New builds one per brain.
type Sensor[E any] interface {
	// Requires lists the memories the sensor writes; the brain registers
	// them.
	Requires() []MemoryID
	// Sense is doTick: it runs once every scan rate ticks.
	Sense(c *Ctx, e E)
}

// DefaultScanRate is Sensor.DEFAULT_SCAN_RATE.
const DefaultScanRate = 20

// SensorType is Java's SensorType: a named factory. ScanRate is the
// constructor argument of the Java sensor (DefaultScanRate when it passes
// none). Key is Paper's config key: the Java class name in lower case
// ("secondarypoisensor"); Paper's tick-rates.sensor table overrides the
// scan rate by entity type and key.
type SensorType[E any] struct {
	Name     string
	Key      string
	ScanRate int
	New      func() Sensor[E]
}

// sensorSlot is a running sensor with its countdown (Sensor.timeToTick).
type sensorSlot[E any] struct {
	s        Sensor[E]
	rate     int
	timeLeft int64
}

// tick is Sensor.tick: count down, then reset to the scan rate (Paper: the
// configured rate) and run.
func (ss *sensorSlot[E]) tick(c *Ctx, e E) {
	ss.timeLeft--
	if ss.timeLeft <= 0 {
		ss.timeLeft = int64(ss.rate)
		ss.s.Sense(c, e)
	}
}

var (
	paperMu            sync.RWMutex
	paperSensorRates   = map[[2]string]int{}
	paperBehaviorRates = map[[2]string]int{}
)

func init() {
	// Paper's default paper-world-defaults.yml:
	//   tick-rates.sensor.villager.secondarypoisensor: 40
	//   tick-rates.behavior.villager.validatenearbypoi: -1 (no override)
	SetPaperSensorRate("villager", "secondarypoisensor", 40)
}

// SetPaperSensorRate sets Paper's tick-rates.sensor.<entity>.<key>; a rate
// below 0 removes the override. entity is the Java entity type path
// ("villager").
func SetPaperSensorRate(entity, key string, rate int) {
	paperMu.Lock()
	defer paperMu.Unlock()
	if rate < 0 {
		delete(paperSensorRates, [2]string{entity, key})
		return
	}
	paperSensorRates[[2]string{entity, key}] = rate
}

// SetPaperBehaviorRate sets Paper's tick-rates.behavior.<entity>.<key>: a
// behaviour may only start again rate ticks after its last end time
// (Paper's Behavior.tryStart patch). Below 0 removes the override.
func SetPaperBehaviorRate(entity, key string, rate int) {
	paperMu.Lock()
	defer paperMu.Unlock()
	if rate < 0 {
		delete(paperBehaviorRates, [2]string{entity, key})
		return
	}
	paperBehaviorRates[[2]string{entity, key}] = rate
}

func paperSensorRate(entity, key string) (int, bool) {
	paperMu.RLock()
	defer paperMu.RUnlock()
	r, ok := paperSensorRates[[2]string{entity, key}]
	return r, ok
}

// PaperBehaviorRate returns the Paper behaviour tick rate for (entity, key),
// or -1.
func PaperBehaviorRate(entity, key string) int {
	paperMu.RLock()
	defer paperMu.RUnlock()
	if r, ok := paperBehaviorRates[[2]string{entity, key}]; ok {
		return r
	}
	return -1
}
