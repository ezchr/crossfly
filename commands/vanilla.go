package commands

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// coreCommands are the basic vanilla commands, in every build.
func coreCommands() []cmd.Command {
	return []cmd.Command{
		cmd.New("gamemode", "Sets a player's game mode.", []string{"gm"}, gameMode{}, gameModeID{}),
		cmd.New("tp", "Teleports players.", []string{"teleport"}, tpToPos{}, tpToTarget{}, tpTargetsToPos{}, tpTargetsToTarget{}),
		cmd.New("give", "Gives an item to players.", nil, give{}),
		cmd.New("clear", "Clears items from a player's inventory.", nil, clearInv{}),
		cmd.New("kill", "Kills players and other entities.", nil, kill{}),
		cmd.New("effect", "Adds or removes status effects.", nil, effectGive{}, effectClear{}),
		cmd.New("time", "Changes or queries the world's game time.", nil, timeSet{}, timeAdd{}, timeQuery{}),
		cmd.New("weather", "Sets the weather.", nil, weatherCmd{}),
		cmd.New("difficulty", "Sets the difficulty level.", nil, difficultyCmd{}),
		cmd.New("setworldspawn", "Sets the world spawn.", nil, setWorldSpawn{}),
		cmd.New("say", "Sends a message in the chat to all players.", nil, say{}),
		cmd.New("tell", "Sends a private message to players.", []string{"msg", "w"}, tell{}),
		cmd.New("me", "Displays a message about yourself.", nil, me{}),
		cmd.New("list", "Lists the players on the server.", nil, list{}),
		cmd.New("kick", "Kicks a player off the server.", nil, kick{}),
		cmd.New("op", "Makes a player an operator.", nil, opCmd{}),
		cmd.New("deop", "Takes operator status from a player.", nil, deopCmd{}),
		cmd.New("stop", "Stops the server.", nil, stop{}),
	}
}

// --- /gamemode ---

type gameModeName string

func (gameModeName) Type() string { return "GameMode" }
func (gameModeName) Options(cmd.Source) []string {
	return []string{"survival", "creative", "adventure", "spectator", "s", "c", "a", "d", "default"}
}

type gameMode struct {
	op
	Mode   gameModeName               `cmd:"gameMode"`
	Target cmd.Optional[[]cmd.Target] `cmd:"player"`
}

type gameModeID struct {
	op
	Mode   int                        `cmd:"gameMode"`
	Target cmd.Optional[[]cmd.Target] `cmd:"player"`
}

func (g gameMode) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	var mode world.GameMode
	switch g.Mode {
	case "survival", "s":
		mode = world.GameModeSurvival
	case "creative", "c":
		mode = world.GameModeCreative
	case "adventure", "a":
		mode = world.GameModeAdventure
	case "spectator":
		mode = world.GameModeSpectator
	case "default", "d":
		mode = tx.World().DefaultGameMode()
	default:
		o.Errorf("Unknown game mode: %v", g.Mode)
		return
	}
	setGameMode(src, o, mode, string(g.Mode), g.Target)
}

func (g gameModeID) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	if g.Mode == 6 {
		g.Mode = 3 // Bedrock's spectator id
	}
	mode, ok := world.GameModeByID(g.Mode)
	if !ok {
		o.Errorf("Unknown game mode: %v", g.Mode)
		return
	}
	setGameMode(src, o, mode, fmt.Sprint(g.Mode), g.Target)
}

func setGameMode(src cmd.Source, o *cmd.Output, mode world.GameMode, name string, t cmd.Optional[[]cmd.Target]) {
	players, ok := targetsOrSelf(src, t, o)
	if !ok {
		return
	}
	for _, p := range players {
		p.SetGameMode(mode)
	}
	o.Printf("Set the game mode of %v to %v", names(players), name)
}

// --- /tp ---

type tpToPos struct {
	op
	Destination mgl64.Vec3 `cmd:"destination"`
}

type tpToTarget struct {
	op
	Destination []cmd.Target `cmd:"destination"`
}

type tpTargetsToPos struct {
	op
	Targets     []cmd.Target `cmd:"victim"`
	Destination mgl64.Vec3   `cmd:"destination"`
}

type tpTargetsToTarget struct {
	op
	Targets     []cmd.Target `cmd:"victim"`
	Destination []cmd.Target `cmd:"destination"`
}

func (t tpToPos) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	teleportSelf(src, o, t.Destination)
}

func (t tpToTarget) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	if len(t.Destination) != 1 {
		o.Error("The destination must be one entity")
		return
	}
	teleportSelf(src, o, t.Destination[0].Position())
}

func (t tpTargetsToPos) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	teleport(o, t.Targets, t.Destination)
}

func (t tpTargetsToTarget) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	if len(t.Destination) != 1 {
		o.Error("The destination must be one entity")
		return
	}
	teleport(o, t.Targets, t.Destination[0].Position())
}

func teleportSelf(src cmd.Source, o *cmd.Output, pos mgl64.Vec3) {
	p, ok := src.(*player.Player)
	if !ok {
		o.Error("A target to teleport is needed when run from the console")
		return
	}
	p.Teleport(pos)
	o.Printf("Teleported %v to %.2f, %.2f, %.2f", p.Name(), pos[0], pos[1], pos[2])
}

func teleport(o *cmd.Output, targets []cmd.Target, pos mgl64.Vec3) {
	n := 0
	for _, t := range targets {
		if e, ok := t.(interface{ Teleport(mgl64.Vec3) }); ok {
			e.Teleport(pos)
			n++
		}
	}
	if n == 0 {
		o.Error("No entity was found")
		return
	}
	o.Printf("Teleported %v entities to %.2f, %.2f, %.2f", n, pos[0], pos[1], pos[2])
}

// --- /give ---

// itemName is an item identifier, suggested from the registered items.
type itemName string

func (itemName) Type() string { return "Item" }
func (itemName) Options(cmd.Source) []string {
	seen := map[string]bool{}
	var names []string
	for _, it := range world.Items() {
		name, _ := it.EncodeItem()
		short := strings.TrimPrefix(name, "minecraft:")
		if !seen[short] {
			seen[short] = true
			names = append(names, short, name)
		}
	}
	slices.Sort(names)
	return names
}

type give struct {
	op
	Targets []cmd.Target      `cmd:"player"`
	Item    itemName          `cmd:"itemName"`
	Amount  cmd.Optional[int] `cmd:"amount"`
	Data    cmd.Optional[int] `cmd:"data"`
}

func (g give) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	name := string(g.Item)
	if !strings.Contains(name, ":") {
		name = "minecraft:" + name
	}
	it, ok := world.ItemByName(name, int16(g.Data.LoadOr(0)))
	if !ok {
		o.Errorf("Unknown item: %v", g.Item)
		return
	}
	amount := g.Amount.LoadOr(1)
	if amount < 1 || amount > 32767 {
		o.Error("The amount must be between 1 and 32767")
		return
	}
	players := playersOf(g.Targets)
	if len(players) == 0 {
		o.Error("No player was found")
		return
	}
	for _, p := range players {
		left := amount
		for left > 0 {
			n := min(left, item.NewStack(it, 1).MaxCount())
			if _, err := p.Inventory().AddItem(item.NewStack(it, n)); err != nil {
				break
			}
			left -= n
		}
	}
	o.Printf("Gave %v * %v to %v", amount, g.Item, names(players))
}

// --- /clear ---

type clearInv struct {
	op
	Targets cmd.Optional[[]cmd.Target] `cmd:"player"`
}

func (c clearInv) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	players, ok := targetsOrSelf(src, c.Targets, o)
	if !ok {
		return
	}
	for _, p := range players {
		p.Inventory().Clear()
		p.Armour().Clear()
		p.SetHeldItems(item.Stack{}, item.Stack{})
	}
	o.Printf("Cleared the inventory of %v", names(players))
}

// --- /kill ---

type kill struct {
	op
	Targets cmd.Optional[[]cmd.Target] `cmd:"target"`
}

func (k kill) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	targets, ok := k.Targets.Load()
	if !ok {
		p, isPlayer := src.(*player.Player)
		if !isPlayer {
			o.Error("A target is needed when run from the console")
			return
		}
		targets = []cmd.Target{p}
	}
	n := 0
	for _, t := range targets {
		switch e := t.(type) {
		case *player.Player:
			if !e.Dead() && e.GameMode() != world.GameModeSpectator {
				e.Kill(entity.VoidDamageSource{})
				n++
			}
		case entity.Living:
			e.Hurt(math.MaxFloat64, entity.VoidDamageSource{})
			n++
		case world.Entity:
			_ = tx.RemoveEntity(e)
			n++
		}
	}
	o.Printf("Killed %v entities", n)
}

// --- /effect ---

// effectNames are the vanilla effect names by Bedrock effect id.
var effectNames = map[string]int{
	"speed": 1, "slowness": 2, "haste": 3, "mining_fatigue": 4, "strength": 5, "instant_health": 6,
	"instant_damage": 7, "jump_boost": 8, "nausea": 9, "regeneration": 10, "resistance": 11, "fire_resistance": 12,
	"water_breathing": 13, "invisibility": 14, "blindness": 15, "night_vision": 16, "hunger": 17, "weakness": 18,
	"poison": 19, "wither": 20, "health_boost": 21, "absorption": 22, "saturation": 23, "levitation": 24,
	"fatal_poison": 25, "conduit_power": 26, "slow_falling": 27, "bad_omen": 28, "village_hero": 29, "darkness": 30,
	"trial_omen": 31, "wind_charged": 32, "weaving": 33, "oozing": 34, "infested": 35, "raid_omen": 36,
}

type effectName string

func (effectName) Type() string { return "Effect" }
func (effectName) Options(cmd.Source) []string {
	var out []string
	for name, id := range effectNames {
		if _, ok := effect.ByID(id); ok {
			out = append(out, name, "minecraft:"+name)
		}
	}
	slices.Sort(out)
	return out
}

type effectGive struct {
	op
	Targets       []cmd.Target       `cmd:"player"`
	Effect        effectName         `cmd:"effect"`
	Seconds       cmd.Optional[int]  `cmd:"seconds"`
	Amplifier     cmd.Optional[int]  `cmd:"amplifier"`
	HideParticles cmd.Optional[bool] `cmd:"hideParticles"`
}

type effectClearArg string

func (effectClearArg) Type() string                { return "EffectClear" }
func (effectClearArg) Options(cmd.Source) []string { return []string{"clear"} }

type effectClear struct {
	op
	Targets []cmd.Target   `cmd:"player"`
	Clear   effectClearArg `cmd:"clear"`
}

func (e effectGive) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	id, ok := effectNames[strings.TrimPrefix(string(e.Effect), "minecraft:")]
	t, found := effect.ByID(id)
	if !ok || !found {
		o.Errorf("Unknown effect: %v", e.Effect)
		return
	}
	players := playersOf(e.Targets)
	if len(players) == 0 {
		o.Error("No player was found")
		return
	}
	level := e.Amplifier.LoadOr(0) + 1
	if level < 1 || level > 256 {
		o.Error("The amplifier must be between 0 and 255")
		return
	}
	seconds := e.Seconds.LoadOr(30)
	for _, p := range players {
		var eff effect.Effect
		if lt, lasting := t.(effect.LastingType); lasting {
			if seconds <= 0 {
				p.RemoveEffect(t)
				continue
			}
			eff = effect.New(lt, level, time.Duration(seconds)*time.Second)
			if e.HideParticles.LoadOr(false) {
				eff = eff.WithoutParticles()
			}
		} else {
			eff = effect.NewInstant(t, level)
		}
		p.AddEffect(eff)
	}
	o.Printf("Applied effect %v to %v", e.Effect, names(players))
}

func (e effectClear) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	players := playersOf(e.Targets)
	for _, p := range players {
		for _, eff := range p.Effects() {
			p.RemoveEffect(eff.Type())
		}
	}
	o.Printf("Took all effects from %v", names(players))
}

// --- /time ---

type timeSetArg string

func (timeSetArg) Type() string                { return "TimeSet" }
func (timeSetArg) Options(cmd.Source) []string { return []string{"set"} }

type timeAddArg string

func (timeAddArg) Type() string                { return "TimeAdd" }
func (timeAddArg) Options(cmd.Source) []string { return []string{"add"} }

type timeQueryArg string

func (timeQueryArg) Type() string                { return "TimeQuery" }
func (timeQueryArg) Options(cmd.Source) []string { return []string{"query"} }

type timeSpec string

func (timeSpec) Type() string { return "TimeSpec" }
func (timeSpec) Options(cmd.Source) []string {
	return []string{"day", "night", "noon", "midnight", "sunrise", "sunset"}
}

type timeSet struct {
	op
	Set  timeSetArg  `cmd:"set"`
	Time cmd.Varargs `cmd:"time"`
}

type timeAdd struct {
	op
	Add    timeAddArg `cmd:"add"`
	Amount int        `cmd:"amount"`
}

type timeQueryKind string

func (timeQueryKind) Type() string                { return "TimeQueryKind" }
func (timeQueryKind) Options(cmd.Source) []string { return []string{"daytime", "gametime", "day"} }

type timeQuery struct {
	op
	Query timeQueryArg  `cmd:"query"`
	Kind  timeQueryKind `cmd:"time"`
}

var namedTimes = map[string]int{"day": 1000, "noon": 6000, "sunset": 12000, "night": 13000, "midnight": 18000, "sunrise": 23000}

func (t timeSet) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	arg := strings.TrimSpace(string(t.Time))
	v, ok := namedTimes[arg]
	if !ok {
		if _, err := fmt.Sscan(arg, &v); err != nil {
			o.Errorf("Unknown time: %v", arg)
			return
		}
	}
	tx.World().SetTime(v)
	o.Printf("Set the time to %v", v)
}

func (t timeAdd) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	w := tx.World()
	w.SetTime(w.Time() + t.Amount)
	o.Printf("Set the time to %v", w.Time())
}

func (t timeQuery) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	tim := tx.World().Time()
	switch t.Kind {
	case "daytime":
		o.Printf("Daytime is %v", tim%24000)
	case "day":
		o.Printf("Day is %v", tim/24000)
	default:
		o.Printf("Time is %v", tim)
	}
}

// --- /weather ---

type weatherKind string

func (weatherKind) Type() string                { return "WeatherType" }
func (weatherKind) Options(cmd.Source) []string { return []string{"clear", "rain", "thunder"} }

type weatherCmd struct {
	op
	Weather  weatherKind       `cmd:"type"`
	Duration cmd.Optional[int] `cmd:"duration"`
}

func (c weatherCmd) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	w := tx.World()
	d := time.Duration(c.Duration.LoadOr(300+int(time.Now().UnixNano()%600))) * time.Second
	switch c.Weather {
	case "clear":
		w.StopRaining()
		w.StopThundering()
	case "rain":
		w.StopThundering()
		w.StartRaining(d)
	case "thunder":
		w.StartRaining(d)
		w.StartThundering(d)
	default:
		o.Errorf("Unknown weather: %v", c.Weather)
		return
	}
	o.Printf("Changed the weather to %v", c.Weather)
}

// --- /difficulty ---

type difficultyName string

func (difficultyName) Type() string { return "Difficulty" }
func (difficultyName) Options(cmd.Source) []string {
	return []string{"peaceful", "easy", "normal", "hard", "p", "e", "n", "h"}
}

type difficultyCmd struct {
	op
	Difficulty difficultyName `cmd:"difficulty"`
}

func (c difficultyCmd) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	var d world.Difficulty
	switch c.Difficulty {
	case "peaceful", "p":
		d = world.DifficultyPeaceful
	case "easy", "e":
		d = world.DifficultyEasy
	case "normal", "n":
		d = world.DifficultyNormal
	case "hard", "h":
		d = world.DifficultyHard
	default:
		o.Errorf("Unknown difficulty: %v", c.Difficulty)
		return
	}
	for _, w := range []*world.World{srv.World(), srv.Nether(), srv.End()} {
		if w == tx.World() {
			w.SetDifficulty(d)
		} else {
			w.Do(func(*world.Tx) { w.SetDifficulty(d) })
		}
	}
	o.Printf("Set the difficulty to %v", c.Difficulty)
}

// --- /setworldspawn ---

type setWorldSpawn struct {
	op
	Position cmd.Optional[mgl64.Vec3] `cmd:"spawnPoint"`
}

func (c setWorldSpawn) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	pos, ok := c.Position.Load()
	if !ok {
		pos = src.Position()
	}
	tx.World().SetSpawn(cube.PosFromVec3(pos))
	o.Printf("Set the world spawn point to %v", cube.PosFromVec3(pos))
}

// --- /say, /tell, /me ---

type say struct {
	op
	Message cmd.Varargs `cmd:"message"`
}

func (s say) Run(src cmd.Source, _ *cmd.Output, _ *world.Tx) {
	_, _ = fmt.Fprintf(chat.Global, "[%v] %v", sourceName(src), s.Message)
}

type tell struct {
	Targets []cmd.Target `cmd:"target"`
	Message cmd.Varargs  `cmd:"message"`
}

func (t tell) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	players := playersOf(t.Targets)
	if len(players) == 0 {
		o.Error("No player was found")
		return
	}
	for _, p := range players {
		p.Messagef("§7§o%v whispers to you: %v", sourceName(src), t.Message)
	}
	o.Printf("§7§oYou whisper to %v: %v", names(players), t.Message)
}

type me struct {
	Message cmd.Varargs `cmd:"action"`
}

func (m me) Run(src cmd.Source, _ *cmd.Output, _ *world.Tx) {
	_, _ = fmt.Fprintf(chat.Global, "* %v %v", sourceName(src), m.Message)
}

func sourceName(src cmd.Source) string {
	if n, ok := src.(cmd.NamedTarget); ok {
		return n.Name()
	}
	return "Server"
}

// --- /list ---

type list struct{}

func (list) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	var n []string
	for p := range srv.Players(tx) {
		n = append(n, p.Name())
	}
	slices.Sort(n)
	if limit, ok := srv.PlayerLimit(); ok {
		o.Printf("There are %v/%v players online:", srv.PlayerCount(), limit)
	} else {
		o.Printf("There are %v players online:", srv.PlayerCount())
	}
	o.Print(strings.Join(n, ", "))
}

// --- /kick ---

type kick struct {
	op
	Targets []cmd.Target              `cmd:"name"`
	Reason  cmd.Optional[cmd.Varargs] `cmd:"reason"`
}

func (k kick) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	players := playersOf(k.Targets)
	if len(players) == 0 {
		o.Error("No player was found")
		return
	}
	reason := string(k.Reason.LoadOr("Kicked by an operator"))
	for _, p := range players {
		p.Disconnect(reason)
	}
	o.Printf("Kicked %v: %v", names(players), reason)
}

// --- /op, /deop ---

type opCmd struct {
	op
	Player cmd.Varargs `cmd:"player"`
}

type deopCmd struct {
	op
	Player cmd.Varargs `cmd:"player"`
}

func (c opCmd) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	name := strings.TrimSpace(string(c.Player))
	if name == "" {
		o.Error("A player name is needed")
		return
	}
	var p *player.Player
	for q := range srv.Players(tx) {
		if strings.EqualFold(q.Name(), name) {
			p = q
		}
	}
	if err := ops.Add(name, p); err != nil {
		o.Errorf("Could not save the operators: %v", err)
		return
	}
	if p != nil {
		p.Message("§eYou are now an operator")
	}
	o.Printf("Made %v a server operator", name)
}

func (c deopCmd) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	name := strings.TrimSpace(string(c.Player))
	removed, err := ops.Remove(name)
	if err != nil {
		o.Errorf("Could not save the operators: %v", err)
		return
	}
	if !removed {
		o.Errorf("%v is not an operator", name)
		return
	}
	for q := range srv.Players(tx) {
		if strings.EqualFold(q.Name(), name) {
			q.Message("§eYou are no longer an operator")
		}
	}
	o.Printf("Made %v no longer a server operator", name)
}

// --- /stop ---

type stop struct{ op }

func (stop) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	o.Print("Stopping the server")
	go func() { _ = srv.Close() }()
}
