//go:build crossfly_full

package block

// lecternPulses reports whether a lectern gives a redstone pulse when its page turns. In Crossfly Full it does with
// both redstone engines: Java Edition's signalPageChange and Bedrock's LecternBlock::emitRedstonePulse both power it
// for two ticks. Comparators read the page (LecternBlock::getComparatorSignal).
func lecternPulses() bool { return true }
