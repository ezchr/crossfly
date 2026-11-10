//go:build crossfly_full

package combat

import (
	"encoding/json"
	"testing"
)

// The camelCase keys of bee.json and wandering_trader.json read like their snake_case names
// (AngryDefinition::buildSchema: "broadcast_anger|broadcastAnger", ...).
func TestAngryCamelCaseAliases(t *testing.T) {
	cases := map[string]struct {
		js       string
		bc       bool
		rng      float64
		targets  int
		delta    int
		hasCalm  bool
		duration int
	}{
		"bee": {js: `{"duration": 25, "broadcastAnger": true, "broadcastRange": 20, "broadcast_anger_when_dying": false, "broadcast_filters": {"test": "is_family", "operator": "!=", "value": "pacified"}, "calm_event": {"event": "calmed_down", "target": "self"}}`,
			bc: true, rng: 20, hasCalm: true, duration: 25},
		"wandering_trader": {js: `{"broadcastAnger": true, "broadcastRange": 10, "calm_event": {"event": "minecraft:become_calm", "target": "self"}, "broadcast_filters": {"subject": "other", "test": "is_leashed_to", "value": true}, "broadcast_targets": ["llama", "trader_llama"], "duration": 5}`,
			bc: true, rng: 10, targets: 2, hasCalm: true, duration: 5},
		"all camelCase": {js: `{"durationDelta": 3, "broadcastAnger": true, "broadcastRange": 7, "broadcastTargets": ["piglin"], "broadcastFilters": {"test": "is_family", "value": "piglin"}, "calmEvent": {"event": "calm"}, "duration": 9}`,
			bc: true, rng: 7, targets: 1, delta: 3, hasCalm: true, duration: 9},
		"snake wins": {js: `{"broadcast_range": 4, "broadcastRange": 30}`, rng: 4, duration: 25},
	}
	for name, c := range cases {
		comp, err := newAngry("minecraft:angry", json.RawMessage(c.js))
		if err != nil {
			t.Fatalf("%v: %v", name, err)
		}
		a := comp.(*Angry)
		if a.broadcast != c.bc || a.broadcastRange != c.rng || len(a.broadcastTargets) != c.targets || a.delta != c.delta || a.duration != c.duration {
			t.Errorf("%v: broadcast %v range %v targets %v delta %v duration %v", name, a.broadcast, a.broadcastRange, a.broadcastTargets, a.delta, a.duration)
		}
		if a.calm.Empty() == c.hasCalm {
			t.Errorf("%v: calm event parsed: %v", name, !a.calm.Empty())
		}
	}
}
