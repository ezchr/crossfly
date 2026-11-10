//go:build crossfly_full

package mob

// hitPartKey keeps the body part a hit about to land struck (SetHitPart).
type hitPartKey struct{}

type hitPart struct {
	part int
	tick int64
}

// SetHitPart records which body part of a multi-part mob (the ender dragon's
// head, neck, body, tail or wings) the hit about to land struck. A Java
// client attacks the part entity, whose id follows the mob's, so the session
// knows the part; Bedrock clients only name the mob. The part holds for the
// current tick only.
func (m *Mob) SetHitPart(part int) {
	m.SetData(hitPartKey{}, hitPart{part: part, tick: m.s.ticks})
}

// TakeHitPart returns the part set with SetHitPart this tick and forgets it.
func (m *Mob) TakeHitPart() (int, bool) {
	v, ok := m.Data(hitPartKey{})
	if !ok {
		return 0, false
	}
	h := v.(hitPart)
	if h.part < 0 || h.tick != m.s.ticks {
		return 0, false
	}
	m.SetData(hitPartKey{}, hitPart{part: -1})
	return h.part, true
}
