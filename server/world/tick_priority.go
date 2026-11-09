package world

// TickPriority orders block ticks due at the same time, matching Java Edition.
// Lower priorities run first; ticks with equal priority keep their scheduling order.
type TickPriority int8

const (
	TickPriorityExtremelyHigh TickPriority = -3
	TickPriorityVeryHigh      TickPriority = -2
	TickPriorityHigh          TickPriority = -1
	TickPriorityNormal        TickPriority = 0
	TickPriorityLow           TickPriority = 1
	TickPriorityVeryLow       TickPriority = 2
	TickPriorityExtremelyLow  TickPriority = 3
)
