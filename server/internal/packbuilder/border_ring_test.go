package packbuilder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

// TestBorderRingSameWall checks that every anchor's copy of the wall is at the same place in the
// world, and that the walls reach the corners and no further.
func TestBorderRingSameWall(t *testing.T) {
	const minX, minZ, maxX, maxZ = 31.0, -179.0, 408.0, 198.0
	model := borderRingGeometry("g", minX, minZ, maxX, maxZ)
	data, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("model is %d KB", len(data)/1024)
	bones := model["minecraft:geometry"].([]any)[0].(map[string]any)["bones"].([]any)
	nx, nz := world.BorderRingCells(minX, maxX), world.BorderRingCells(minZ, maxZ)
	if len(bones) != nx*nz+1 {
		t.Fatalf("%d bones for %d anchors", len(bones), nx*nz)
	}
	var want map[string]bool
	for a, b := range bones[1:] {
		ax, az := world.BorderRingAnchor(minX, a/nz), world.BorderRingAnchor(minZ, a%nz)
		got := map[string]bool{}
		for _, c := range b.(map[string]any)["cubes"].([]any) {
			cube := c.(map[string]any)
			o, s := cube["origin"].([]float64), cube["size"].([]float64)
			// A model's x is the world's, its z the world's backwards.
			x0, x1 := ax+o[0]/16, ax+(o[0]+s[0])/16
			z1, z0 := az-o[2]/16, az-(o[2]+s[2])/16
			if x0 < minX || x1 > maxX || z0 < minZ || z1 > maxZ {
				t.Fatalf("anchor %d: cube past the corners: x %v..%v z %v..%v", a, x0, x1, z0, z1)
			}
			if x0 != x1 && z0 != minZ && z0 != maxZ || z0 != z1 && x0 != minX && x0 != maxX {
				t.Fatalf("anchor %d: cube off the wall: x %v..%v z %v..%v", a, x0, x1, z0, z1)
			}
			got[fmt.Sprint(x0, x1, z0, z1, o[1])] = true
		}
		if want == nil {
			want = got
			continue
		}
		if len(got) != len(want) {
			t.Fatalf("anchor %d has %d cubes, not %d", a, len(got), len(want))
		}
		for k := range got {
			if !want[k] {
				t.Fatalf("anchor %d differs: %s", a, k)
			}
		}
	}
	covered := map[string]float64{}
	for k := range want {
		var x0, x1, z0, z1, y float64
		fmt.Sscan(k, &x0, &x1, &z0, &z1, &y)
		if y != 0 {
			continue
		}
		switch {
		case z0 == z1 && z0 == minZ:
			covered["north"] += x1 - x0
		case z0 == z1:
			covered["south"] += x1 - x0
		case x0 == minX:
			covered["west"] += z1 - z0
		default:
			covered["east"] += z1 - z0
		}
	}
	for side, length := range covered {
		if length != 377 {
			t.Fatalf("%s side is %v long", side, length)
		}
	}
	if len(covered) != 4 {
		t.Fatalf("sides: %v", covered)
	}
}

// TestBorderControllersVanilla checks what vanilla needs of a render controller, and other clients
// may not: without an offset and a scale in uv_anim, it refuses the controller and draws nothing.
func TestBorderControllersVanilla(t *testing.T) {
	world.UseBorderWall()
	world.SetBorderRing(31, -179, 408, 198)
	dir := t.TempDir()
	buildBorder(dir)
	for _, name := range []string{"world_border", "world_border_cap"} {
		data, err := os.ReadFile(filepath.Join(dir, "render_controllers", name+".render_controllers.json"))
		if err != nil {
			t.Fatal(err)
		}
		var file struct {
			Controllers map[string]struct {
				UVAnim map[string][]any `json:"uv_anim"`
			} `json:"render_controllers"`
		}
		if err := json.Unmarshal(data, &file); err != nil {
			t.Fatal(err)
		}
		c, ok := file.Controllers["controller.render.dragonfly."+name]
		if !ok {
			t.Fatalf("%s: no controller", name)
		}
		for _, key := range []string{"offset", "scale"} {
			if len(c.UVAnim[key]) != 2 {
				t.Fatalf("%s: uv_anim %s is %v", name, key, c.UVAnim[key])
			}
		}
	}
}
