//go:build crossfly_full

package javamap

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"sync"
)

//go:embed block_state_data.json.gz
var blockStateData []byte

type blockStateDescription struct {
	Name       string            `json:"n"`
	Properties map[string]string `json:"p,omitempty"`
}

var blockDescriptions = sync.OnceValue(func() []blockStateDescription {
	reader, err := gzip.NewReader(bytes.NewReader(blockStateData))
	if err != nil {
		panic(err)
	}
	defer reader.Close()
	var descriptions []blockStateDescription
	if err := json.NewDecoder(reader).Decode(&descriptions); err != nil {
		panic(err)
	}
	return descriptions
})

// BlockStateData returns the Java BlockState codec representation of a native 26.3 state ID. Moving piston block
// entities use this codec rather than the runtime ID in their NBT. The generated data comes from the same report
// as blocks_gen.go, including exact block properties.
func BlockStateData(id int32) (map[string]any, bool) {
	states := blockDescriptions()
	if id < 0 || int(id) >= len(states) {
		return nil, false
	}
	state := states[id]
	out := map[string]any{"Name": state.Name}
	if len(state.Properties) > 0 {
		props := make(map[string]any, len(state.Properties))
		for name, value := range state.Properties {
			props[name] = value
		}
		out["Properties"] = props
	}
	return out, true
}
