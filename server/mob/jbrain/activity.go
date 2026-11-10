package jbrain

import "sync"

// Activity is a brain activity (Java net.minecraft.world.entity.schedule.Activity).
// Activities are small registered ids so that a brain keeps its active set in
// a bit set.
type Activity uint8

var (
	actMu    sync.RWMutex
	actNames []string
	actHash  []int32
	actByNm  = map[string]Activity{}
)

// RegisterActivity registers a new activity. Register only from init; at most
// 64 activities exist.
func RegisterActivity(name string) Activity {
	actMu.Lock()
	defer actMu.Unlock()
	if _, ok := actByNm[name]; ok {
		panic("jbrain: activity " + name + " registered twice")
	}
	if len(actNames) >= 64 {
		panic("jbrain: too many activities")
	}
	a := Activity(len(actNames))
	actNames = append(actNames, name)
	actHash = append(actHash, javaStringHash(name))
	actByNm[name] = a
	return a
}

// ActivityByName looks up a registered activity.
func ActivityByName(name string) (Activity, bool) {
	actMu.RLock()
	defer actMu.RUnlock()
	a, ok := actByNm[name]
	return a, ok
}

// String returns the activity's name ("idle").
func (a Activity) String() string {
	actMu.RLock()
	defer actMu.RUnlock()
	if int(a) < len(actNames) {
		return actNames[a]
	}
	return "unknown"
}

// javaHash is Activity.hashCode: the hash of its name.
func (a Activity) javaHash() int32 {
	actMu.RLock()
	defer actMu.RUnlock()
	return actHash[a]
}

// The vanilla activities, in Java's registration order.
var (
	ActCore        = RegisterActivity("core")
	ActIdle        = RegisterActivity("idle")
	ActWork        = RegisterActivity("work")
	ActPlay        = RegisterActivity("play")
	ActRest        = RegisterActivity("rest")
	ActMeet        = RegisterActivity("meet")
	ActPanic       = RegisterActivity("panic")
	ActRaid        = RegisterActivity("raid")
	ActPreRaid     = RegisterActivity("pre_raid")
	ActHide        = RegisterActivity("hide")
	ActFight       = RegisterActivity("fight")
	ActCelebrate   = RegisterActivity("celebrate")
	ActAdmireItem  = RegisterActivity("admire_item")
	ActAvoid       = RegisterActivity("avoid")
	ActRide        = RegisterActivity("ride")
	ActPlayDead    = RegisterActivity("play_dead")
	ActLongJump    = RegisterActivity("long_jump")
	ActRam         = RegisterActivity("ram")
	ActTongue      = RegisterActivity("tongue")
	ActSwim        = RegisterActivity("swim")
	ActLaySpawn    = RegisterActivity("lay_spawn")
	ActSniff       = RegisterActivity("sniff")
	ActInvestigate = RegisterActivity("investigate")
	ActRoar        = RegisterActivity("roar")
	ActEmerge      = RegisterActivity("emerge")
	ActDig         = RegisterActivity("dig")
)

// activitySet is a set of activities.
type activitySet uint64

func (s activitySet) has(a Activity) bool { return s&(1<<a) != 0 }
func (s *activitySet) add(a Activity)     { *s |= 1 << a }
func setOf(acts ...Activity) (s activitySet) {
	for _, a := range acts {
		s.add(a)
	}
	return s
}

// javaStringHash is java.lang.String.hashCode.
func javaStringHash(s string) int32 {
	var h int32
	for _, r := range s {
		if r >= 0x10000 {
			r -= 0x10000
			h = 31*h + (0xD800 + (r >> 10))
			h = 31*h + (0xDC00 + (r & 0x3FF))
			continue
		}
		h = 31*h + r
	}
	return h
}

// javaBucket is the bucket a key with hash h falls into in a java.util.HashMap
// with 16 buckets (HashMap.hash spreads the high bits down).
func javaBucket(h int32) int {
	u := uint32(h)
	return int((u ^ (u >> 16)) & 15)
}
