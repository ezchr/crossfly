//go:build crossfly_full

package animal

// Ageable is the server state of a Java AgeableMob (net.minecraft.world.entity.AgeableMob): the age that
// makes a mob a baby (below 0) or keeps an adult from breeding again (above 0), the forced growth from
// feeding and the golden dandelion age lock.
//
// The zero value is an adult that can breed. All ages are in ticks.
type Ageable struct {
	// age: AgeableMob.age. Below 0 the mob is a baby and counts up to 0; above 0 it is an adult on
	// breeding cooldown and counts down to 0.
	age int32
	// forcedAge: AgeableMob.forcedAge, the ticks of growth forced by feeding. An adult keeps it as its
	// age (AgeableMob.ageUp sets the age to forcedAge once it reaches 0).
	forcedAge int32
	// forcedAgeTimer: AgeableMob.forcedAgeTimer, the ticks left of happy villager particles after feeding.
	forcedAgeTimer int32
	// ageLockParticleTimer: AgeableMob.ageLockParticleTimer, the ticks left of the age lock particles,
	// which is also the golden dandelion cooldown.
	ageLockParticleTimer int32
	// ageLocked: AgeableMob.AGE_LOCKED, a baby fed a golden dandelion that never grows up.
	ageLocked bool
	// NoBaby is set for mobs whose canBeABaby() is false: they are never babies and save no age.
	NoBaby bool
}

const (
	// BabyStartAge is AgeableMob.BABY_START_AGE: a baby grows up in 20 minutes.
	BabyStartAge int32 = -24000
	// AgeLockCooldownTicks is AgeableMob.AGE_LOCK_COOLDOWN_TICKS, the golden dandelion cooldown.
	AgeLockCooldownTicks int32 = 40
	// forcedAgeParticleTicks is AgeableMob.FORCED_AGE_PARTICLE_TICKS.
	forcedAgeParticleTicks int32 = 40
)

// AgeTick is what one tick of growth did (AgeableMob.aiStep), for the caller to show.
type AgeTick struct {
	// HappyParticle: send one happy villager particle (every 4th tick of forced growth).
	HappyParticle bool
	// AgeLockParticle: send one pause_mob_growth (AgeLocked) or reset_mob_growth particle above the
	// mob (every 2nd tick of the age lock timer, AgeableMob.makeAgeLockedParticle).
	AgeLockParticle bool
	// AgeLocked is the lock state the age lock particle shows.
	AgeLocked bool
	// Boundary: the mob grew up (or became a baby): the baby flag changed (AgeableMob.setAge ->
	// ageBoundaryReached). The caller refreshes the size and the entity data.
	Boundary bool
}

// Age is AgeableMob.getAge on the server.
func (a *Ageable) Age() int32 { return a.age }

// ForcedAge is AgeableMob.getForcedAge.
func (a *Ageable) ForcedAge() int32 { return a.forcedAge }

// Baby is AgeableMob.isBaby: the mob can be a baby and its age is below 0.
func (a *Ageable) Baby() bool { return !a.NoBaby && a.age < 0 }

// AgeLocked is AgeableMob.isAgeLocked.
func (a *Ageable) AgeLocked() bool { return a.ageLocked }

// SetAge is AgeableMob.setAge. It reports whether the mob crossed the baby boundary.
func (a *Ageable) SetAge(n int32) (boundary bool) {
	old := a.age
	a.age = n
	return (old < 0) != (n < 0)
}

// SetBaby is AgeableMob.setBaby: BabyStartAge for a baby, 0 for an adult. Mobs that cannot be babies
// keep their age.
func (a *Ageable) SetBaby(baby bool) (boundary bool) {
	if a.NoBaby {
		return false
	}
	if baby {
		return a.SetAge(BabyStartAge)
	}
	return a.SetAge(0)
}

// CanAgeUp is AgeableMob.canAgeUp: a baby that is not age locked.
func (a *Ageable) CanAgeUp() bool { return a.Baby() && !a.ageLocked }

// AgeUp is AgeableMob.ageUp(seconds, forced): the mob grows by seconds (never past 0). Forced growth
// (feeding) is remembered in forcedAge and starts the happy villager particles. An adult reached this
// way takes forcedAge as its age.
func (a *Ageable) AgeUp(seconds int32, forced bool) (boundary bool) {
	old := a.age
	age := old + seconds*20
	if age > 0 {
		age = 0
	}
	delta := age - old
	boundary = a.SetAge(age)
	if forced {
		a.forcedAge += delta
		if a.forcedAgeTimer == 0 {
			a.forcedAgeTimer = forcedAgeParticleTicks
		}
	}
	if a.age == 0 {
		if a.SetAge(a.forcedAge) {
			boundary = !boundary
		}
	}
	return boundary
}

// SpeedUpSecondsWhenFeeding is AgeableMob.getSpeedUpSecondsWhenFeeding: feeding a baby takes 10% off the
// time it still needs to grow up.
func SpeedUpSecondsWhenFeeding(ticksUntilAdult int32) int32 {
	return int32(float32(ticksUntilAdult/20) * 0.1)
}

// Tick runs the server half of AgeableMob.aiStep once per tick (after the mob's AI): the forced growth
// particles, growing up or counting the breeding cooldown down, and the age lock particles. alive is
// LivingEntity.isAlive.
func (a *Ageable) Tick(alive bool) (t AgeTick) {
	if a.forcedAgeTimer > 0 {
		t.HappyParticle = a.forcedAgeTimer%4 == 0
		a.forcedAgeTimer--
	}
	if alive {
		if a.CanAgeUp() {
			t.Boundary = a.SetAge(a.age + 1)
		} else if a.age > 0 {
			t.Boundary = a.SetAge(a.age - 1)
		}
	}
	if a.ageLockParticleTimer > 0 {
		t.AgeLockParticle = a.ageLockParticleTimer%2 == 0
		t.AgeLocked = a.ageLocked
		a.ageLockParticleTimer--
	}
	return t
}

// CanUseGoldenDandelion is AgeableMob.canUseGoldenDandelion for a mob that is not in the
// minecraft:cannot_be_age_locked entity tag: a baby whose golden dandelion cooldown is over.
func (a *Ageable) CanUseGoldenDandelion() bool {
	return a.Baby() && a.ageLockParticleTimer == 0
}

// ToggleAgeLock is AgeableMob.setAgeLockedData, run when a player feeds a golden dandelion: the lock flips,
// the mob becomes a fresh baby again and the cooldown starts. It returns the new lock state (the caller
// makes a locked mob persistent and plays golden_dandelion_use or _unuse).
func (a *Ageable) ToggleAgeLock() (locked bool) {
	a.ageLocked = !a.ageLocked
	a.SetAge(BabyStartAge)
	a.ageLockParticleTimer = AgeLockCooldownTicks
	return a.ageLocked
}

// ForcedAgeTimer is AgeableMob.getForcedAgeTimer.
func (a *Ageable) ForcedAgeTimer() int32 { return a.forcedAgeTimer }

// EncodeNBT adds the AgeableMob save data (AgeableMob.addAdditionalSaveData) to m.
func (a *Ageable) EncodeNBT(m map[string]any) {
	if a.NoBaby {
		return
	}
	m["Age"] = a.age
	m["ForcedAge"] = a.forcedAge
	m["AgeLocked"] = boolByte(a.ageLocked)
}

// DecodeNBT reads what EncodeNBT wrote (AgeableMob.readAdditionalSaveData).
func (a *Ageable) DecodeNBT(m map[string]any) {
	if a.NoBaby {
		return
	}
	a.age = nbtInt(m, "Age", 0)
	a.forcedAge = nbtInt(m, "ForcedAge", 0)
	a.ageLocked = nbtBool(m, "AgeLocked", false)
}
