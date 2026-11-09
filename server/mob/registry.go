//go:build crossfly_full

package mob

import (
	"cmp"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// All Register*/Set* functions below must be called from init functions
// (a package's register.go) only: the registries are read without locks by
// every world's goroutine once mobs tick. Registering a key twice panics, so
// two packages can never silently claim the same component.

var (
	componentFactories = map[string]ComponentFactory{}
	goalFactories      = map[string]GoalFactory{}
	systems            [stageCount][]System
	initHooks          []InitHook

	travelHook     func(m *Mob)
	knockBackHook  func(m *Mob, src mgl64.Vec3, force, height float64)
	movementSender func(m *Mob)
)

// RegisterComponent registers the factory for a non-goal component key, e.g.
// "minecraft:navigation.walk".
func RegisterComponent(key string, f ComponentFactory) {
	if _, ok := componentFactories[key]; ok {
		panic(fmt.Sprintf("mob: component %q registered twice", key))
	}
	if _, ok := goalFactories[key]; ok {
		panic(fmt.Sprintf("mob: %q already registered as a goal", key))
	}
	componentFactories[key] = f
}

// RegisterGoal registers the factory for a goal key, e.g.
// "minecraft:behavior.random_stroll".
func RegisterGoal(key string, f GoalFactory) {
	if _, ok := goalFactories[key]; ok {
		panic(fmt.Sprintf("mob: goal %q registered twice", key))
	}
	if _, ok := componentFactories[key]; ok {
		panic(fmt.Sprintf("mob: %q already registered as a component", key))
	}
	goalFactories[key] = f
}

// Supported reports whether a component or goal key has a factory.
func Supported(key string) bool {
	_, c := componentFactories[key]
	_, g := goalFactories[key]
	return c || g
}

// IsGoalKey reports whether key is registered as a goal.
func IsGoalKey(key string) bool { _, ok := goalFactories[key]; return ok }

// RegisteredComponents and RegisteredGoals list the registered keys, sorted.
func RegisteredComponents() []string { return slices.Sorted(maps.Keys(componentFactories)) }
func RegisteredGoals() []string      { return slices.Sorted(maps.Keys(goalFactories)) }

// System is a per-mob function run every tick in one Stage. Within a stage,
// the built-in step runs first, then systems by (Order, Name), then the
// stage's component Tickers in definition order.
type System struct {
	Name  string
	Stage Stage
	Order int
	// Tick runs for every alive mob (and, for StageTravel, StageSend and
	// StageDespawn, dying ones too).
	Tick func(m *Mob)
}

// RegisterSystem adds a system to its stage.
func RegisterSystem(s System) {
	if s.Stage >= stageCount || s.Tick == nil || s.Name == "" {
		panic(fmt.Sprintf("mob: invalid system %+v", s))
	}
	for _, o := range systems[s.Stage] {
		if o.Name == s.Name {
			panic(fmt.Sprintf("mob: system %q registered twice", s.Name))
		}
	}
	systems[s.Stage] = append(systems[s.Stage], s)
	slices.SortStableFunc(systems[s.Stage], func(a, b System) int {
		return cmp.Or(cmp.Compare(a.Order, b.Order), cmp.Compare(a.Name, b.Name))
	})
}

// InitHook runs once per mob, when it is initialised (first tick, or Spawn),
// before its components attach. Packages use it to install per-mob defaults
// (package mob/control sets the look and body controls every mob has).
type InitHook struct {
	Name  string
	Order int
	Init  func(m *Mob)
}

// RegisterInitHook adds an init hook.
func RegisterInitHook(h InitHook) {
	initHooks = append(initHooks, h)
	slices.SortStableFunc(initHooks, func(a, b InitHook) int {
		return cmp.Or(cmp.Compare(a.Order, b.Order), cmp.Compare(a.Name, b.Name))
	})
}

// SetTravel installs the travel physics (StageTravel): it moves the mob by
// its velocity and input, with collision, and updates Mob.Physics. Only
// package mob/physics (C) calls it. Without it a mob falls with plain
// gravity and block collision (entity.MovementComputer).
func SetTravel(f func(m *Mob)) {
	if travelHook != nil {
		panic("mob: travel already set")
	}
	travelHook = f
}

// SetKnockBack installs Mob.KnockBack. Only package mob/physics (C) calls it.
func SetKnockBack(f func(m *Mob, src mgl64.Vec3, force, height float64)) {
	if knockBackHook != nil {
		panic("mob: knock back already set")
	}
	knockBackHook = f
}

// SetMovementSender replaces how StageSend shows the mob's movement to
// viewers (conditional_bandwidth_optimization). The default is
// Mob.SendMovement.
func SetMovementSender(f func(m *Mob)) {
	if movementSender != nil {
		panic("mob: movement sender already set")
	}
	movementSender = f
}

// unsupported records keys mobs used without a factory, with how often.
var (
	unsupportedMu sync.Mutex
	unsupported   = map[string]int{}
	factoryErrors = map[string]error{}
)

func recordUnsupported(key string) {
	unsupportedMu.Lock()
	unsupported[key]++
	unsupportedMu.Unlock()
}

func recordFactoryError(id, key string, err error) {
	unsupportedMu.Lock()
	defer unsupportedMu.Unlock()
	k := id + " " + key
	if _, ok := factoryErrors[k]; !ok {
		factoryErrors[k] = err
		slog.Default().Warn("mob: component failed to build", "entity", id, "component", key, "err", err)
	}
}

// UnsupportedSeen returns the component keys that live mobs had active
// without a registered factory, with how many times each was attached.
func UnsupportedSeen() map[string]int {
	unsupportedMu.Lock()
	defer unsupportedMu.Unlock()
	return maps.Clone(unsupported)
}

// FactoryErrors returns the "<entity> <component>" pairs whose factory
// failed, with the error.
func FactoryErrors() map[string]error {
	unsupportedMu.Lock()
	defer unsupportedMu.Unlock()
	return maps.Clone(factoryErrors)
}

// HurtHook runs for every mob after damage was dealt (after the HurtListener
// components), whatever its components: packages that track all hits
// (despawn timers, statistics) use it.
type HurtHook struct {
	Name string
	Hurt func(m *Mob, dealt float64, src world.DamageSource)
}

var hurtHooks []HurtHook

// RegisterHurtHook adds a hurt hook (from init only).
func RegisterHurtHook(h HurtHook) {
	if h.Hurt == nil || h.Name == "" {
		panic(fmt.Sprintf("mob: invalid hurt hook %+v", h))
	}
	for _, o := range hurtHooks {
		if o.Name == h.Name {
			panic(fmt.Sprintf("mob: hurt hook %q registered twice", h.Name))
		}
	}
	hurtHooks = append(hurtHooks, h)
}

// DeathHook runs for every mob that dies (Hurt to 0 or Kill), after its
// DeathListener components, whatever its components: mob-wide death drops
// (BDS Mob::dropEquipmentOnDeath, which every mob runs) use it.
type DeathHook struct {
	Name string
	Die  func(m *Mob, src world.DamageSource)
}

var deathHooks []DeathHook

// RegisterDeathHook adds a death hook (from init only).
func RegisterDeathHook(h DeathHook) {
	if h.Die == nil || h.Name == "" {
		panic(fmt.Sprintf("mob: invalid death hook %+v", h))
	}
	for _, o := range deathHooks {
		if o.Name == h.Name {
			panic(fmt.Sprintf("mob: death hook %q registered twice", h.Name))
		}
	}
	deathHooks = append(deathHooks, h)
}
