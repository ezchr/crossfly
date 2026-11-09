package cmd

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/go-gl/mathgl/mgl64"
)

type fakeTarget struct {
	name string
	pos  mgl64.Vec3
}

func (f fakeTarget) Position() mgl64.Vec3 { return f.pos }
func (f fakeTarget) Name() string         { return f.name }

func TestParseSelector(t *testing.T) {
	src := mgl64.Vec3{10, 64, 10}
	kind, sel, err := parseSelector("@E[type=!custom:golem, r=5,c=-2,x=~1,y=70,dx=3]", src)
	if err != nil || kind != "@e" {
		t.Fatalf("kind %q err %v", kind, err)
	}
	if sel.origin != (mgl64.Vec3{11, 70, 10}) || !sel.hasR || sel.r != 5 || sel.count != -2 || !sel.hasVol || sel.volume[0] != 3 {
		t.Errorf("parsed %+v", sel)
	}
	if len(sel.types) != 1 || !sel.types[0].negate || sel.types[0].value != "custom:golem" {
		t.Errorf("types %+v", sel.types)
	}
	for _, bad := range []string{"@e[type=golem", "@e[tag=x]", "@e[c=two]", "@e[r]"} {
		if _, _, err := parseSelector(bad, src); err == nil {
			t.Errorf("%q parsed without error", bad)
		}
	}
}

func TestTypeMatches(t *testing.T) {
	for _, c := range []struct {
		id, want string
		ok       bool
	}{
		{"custom:golem", "golem", true}, {"custom:golem", "custom:golem", true}, {"custom:golem", "Custom:Golem", true},
		{"minecraft:item", "item", true}, {"custom:golem", "minecraft:golem", false}, {"minecraft:player", "golem", false},
	} {
		if typeMatches(c.id, c.want) != c.ok {
			t.Errorf("typeMatches(%q, %q) != %v", c.id, c.want, c.ok)
		}
	}
}

func TestSelectTargets(t *testing.T) {
	src := mgl64.Vec3{}
	pool := []Target{
		fakeTarget{"far", mgl64.Vec3{20, 0, 0}},
		fakeTarget{"near", mgl64.Vec3{2, 0, 0}},
		fakeTarget{"mid", mgl64.Vec3{8, 0, 0}},
	}
	names := func(s string) (out []string) {
		kind, sel, err := parseSelector(s, src)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		for _, t := range sel.selectTargets(kind, pool) {
			out = append(out, t.(fakeTarget).name)
		}
		return
	}
	check := func(s string, want ...string) {
		got := names(s)
		if len(got) != len(want) {
			t.Errorf("%s = %v, want %v", s, got, want)
			return
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%s = %v, want %v", s, got, want)
				return
			}
		}
	}
	check("@e", "far", "near", "mid")
	check("@p", "near")
	check("@e[c=2]", "near", "mid")
	check("@e[c=-1]", "far")
	check("@e[r=10]", "near", "mid")
	check("@e[rm=5]", "far", "mid")
	check("@e[name=!mid,c=5]", "near", "far")
	check("@e[x=19,y=0,z=0,dx=2,dy=0,dz=0]", "far")
	check("@r[name=mid]", "mid")
}

func TestJavaSelectorArguments(t *testing.T) {
	src := mgl64.Vec3{}
	_, sel, err := parseSelector("@e[type=villager,distance=..10,limit=3,sort=furthest]", src)
	if err != nil {
		t.Fatal(err)
	}
	if !sel.hasR || sel.r != 10 || sel.hasRM || !sel.hasCount || sel.count != 3 || sel.sort != "furthest" {
		t.Fatalf("parsed %+v", sel)
	}
	_, sel, err = parseSelector("@a[distance=2..5]", src)
	if err != nil || sel.rm != 2 || sel.r != 5 {
		t.Fatalf("range: %+v %v", sel, err)
	}
	_, sel, err = parseSelector("@a[distance=4]", src)
	if err != nil || sel.rm != 4 || sel.r != 4 {
		t.Fatalf("exact: %+v %v", sel, err)
	}
	for _, bad := range []string{"@e[limit=0]", "@e[distance=..]", "@e[distance=-1]", "@e[sort=closest]", "@e[tag=x]"} {
		if _, _, err := parseSelector(bad, src); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
}

type uuidTarget struct {
	fakeTarget
	id uuid.UUID
}

func (u uuidTarget) UUID() uuid.UUID { return u.id }

// A target may be given as a UUID, as Java Edition clients do for the entity under the crosshair.
func TestUUIDTargets(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	pool := []Target{fakeTarget{"no uuid", mgl64.Vec3{}}, uuidTarget{fakeTarget{"a", mgl64.Vec3{}}, a}, uuidTarget{fakeTarget{"b", mgl64.Vec3{}}, b}}
	for _, s := range []string{b.String(), strings.ToUpper(b.String())} {
		id, ok := parseUUID(s)
		if !ok {
			t.Fatalf("%s is not parsed as a UUID", s)
		}
		got, ok := targetByUUID(pool, id)
		if !ok || got.(uuidTarget).name != "b" {
			t.Fatalf("%s matched %v, %v", s, got, ok)
		}
	}
	if _, ok := targetByUUID(pool, uuid.New()); ok {
		t.Fatal("an unknown UUID matched a target")
	}
	for _, s := range []string{"Steve", "TestPlayer", strings.ReplaceAll(a.String(), "-", ""), "@a"} {
		if _, ok := parseUUID(s); ok {
			t.Errorf("%q parsed as a UUID", s)
		}
	}
}

// A Java client knows a player with a name of over 16 characters by its first 16.
func TestParsePlayerJavaName(t *testing.T) {
	players := []NamedTarget{fakeTarget{"Ezchr", mgl64.Vec3{}}, fakeTarget{"AVeryLongGamertag1", mgl64.Vec3{}}, fakeTarget{"TwinsWithLongNameA", mgl64.Vec3{}}, fakeTarget{"TwinsWithLongNameB", mgl64.Vec3{}}}
	for name, want := range map[string]string{"ezchr": "Ezchr", "AVeryLongGamertag1": "AVeryLongGamertag1", "averylonggamerta": "AVeryLongGamertag1", "TwinsWithLongNam": "", "AVeryLongGamert": ""} {
		got, err := parser{}.parsePlayer(name, players)
		switch {
		case want == "" && err == nil:
			t.Errorf("%q matched %v", name, got)
		case want != "" && (err != nil || got.(fakeTarget).name != want):
			t.Errorf("%q: %v, %v; want %s", name, got, err, want)
		}
	}
}
