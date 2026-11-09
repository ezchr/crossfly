//go:build crossfly_full

package combat

import "testing"

// The vanilla projectile definitions read as BDS reads them.
func TestProjectileSpecs(t *testing.T) {
	get := func(id string) *projectileSpec {
		s := projectileSpecOf(id)
		if s == nil {
			t.Fatalf("no minecraft:projectile for %v", id)
		}
		return s
	}
	sf := get("minecraft:small_fireball")
	if sf.power != 1.3 || sf.gravity != 0 || sf.inertia != 1 || sf.uncBase != 10 || sf.anchor != 2 {
		t.Errorf("small_fireball flight %+v", sf)
	}
	if sf.pickable || sf.explode != nil || !sf.blockFire || sf.impact == nil || sf.impact.min != 5 || !sf.impact.catchFire {
		t.Errorf("small_fireball hit %+v %+v", sf, sf.impact)
	}
	fb := get("minecraft:fireball")
	if !fb.pickable || !fb.reflectOnHurt || fb.explode == nil || fb.explode.power != 1 || !fb.explode.fire || fb.impact.min != 6 || fb.impact.knockback {
		t.Errorf("fireball %+v %+v %+v", fb, fb.explode, fb.impact)
	}
	if fb.width != 1 || fb.offset[1] != -1.5 {
		t.Errorf("fireball box %v offset %v", fb.width, fb.offset)
	}
	ls := get("minecraft:llama_spit")
	if ls.gravity != 0.06 || ls.impact.min != 1 || ls.impact.catchFire || ls.uncMult != 4 || ls.anchor != 1 {
		t.Errorf("llama_spit %+v %+v", ls, ls.impact)
	}
	sb := get("minecraft:shulker_bullet")
	if !sb.homing || !sb.destroyOnHurt || !sb.pickable || len(sb.effects) != 1 || sb.effects[0].name != "levitation" || sb.effects[0].ticks != [3]int{200, 200, 200} {
		t.Errorf("shulker_bullet %+v", sb)
	}
	if sb.particle != "largeexplode" || !sb.partOther || sb.hitSound != "bullet.hit" {
		t.Errorf("shulker_bullet hit particle %q sound %q", sb.particle, sb.hitSound)
	}
	tr := get("minecraft:thrown_trident")
	if !tr.stick || tr.impact.min != 8 || tr.power != 4 {
		t.Errorf("thrown_trident %+v", tr)
	}
	for _, id := range []string{"minecraft:small_fireball", "minecraft:fireball", "minecraft:llama_spit", "minecraft:shulker_bullet", "minecraft:thrown_trident"} {
		if !definedProjectile(id) {
			t.Errorf("%v is not fired from its definition", id)
		}
	}
	for _, id := range []string{"minecraft:arrow", "minecraft:snowball", "minecraft:splash_potion"} {
		if definedProjectile(id) {
			t.Errorf("%v should keep its Dragonfly entity", id)
		}
	}
}
