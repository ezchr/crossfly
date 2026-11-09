//go:build crossfly_full

package mob

// NBTHook saves and loads mob state that no component holds, such as the
// trades a villager hands to the zombie villager it turns into (package
// village): Encode's keys are merged into the mob's NBT, Decode gets the
// whole NBT after the components loaded theirs.
type NBTHook struct {
	Name   string
	Encode func(m *Mob) map[string]any
	Decode func(m *Mob, d map[string]any)
}

var nbtHooks []NBTHook

// RegisterNBTHook adds h. Call it from an init function.
func RegisterNBTHook(h NBTHook) { nbtHooks = append(nbtHooks, h) }

// encodeNBTHooks merges the hooks' NBT into out.
func (s *state) encodeNBTHooks(out map[string]any) {
	if len(nbtHooks) == 0 {
		return
	}
	m := s.cur
	if m == nil {
		m = &Mob{data: s.data, s: s}
	}
	for _, h := range nbtHooks {
		if h.Encode == nil {
			continue
		}
		for k, v := range h.Encode(m) {
			out[k] = v
		}
	}
}

// decodeNBTHooks hands the NBT to the hooks.
func (m *Mob) decodeNBTHooks(d map[string]any) {
	for _, h := range nbtHooks {
		if h.Decode != nil {
			h.Decode(m, d)
		}
	}
}
