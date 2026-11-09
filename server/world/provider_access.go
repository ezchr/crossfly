package world

// Provider returns the Provider the World loads and saves its chunks and settings with (the one in its
// Config, not the wrapper that serialises the World's own calls). Features that keep level-wide data of their
// own assert it to the optional interface they need (mcdb.DB.LevelData), which must be safe for concurrent use.
func (w *World) Provider() Provider {
	if l, ok := w.conf.Provider.(*lockedProvider); ok {
		return l.p
	}
	return w.conf.Provider
}
