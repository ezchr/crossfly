//go:build crossfly_full

package mob

// SetInvisible and SetVisible let the invisibility effect hide the mob
// (effect.Invisibility calls them on the entity it was added to).
func (m *Mob) SetInvisible() { m.UpdateMeta(func(meta *Meta) { meta.Invisible = true }) }

// SetVisible shows the mob again.
func (m *Mob) SetVisible() { m.UpdateMeta(func(meta *Meta) { meta.Invisible = false }) }
