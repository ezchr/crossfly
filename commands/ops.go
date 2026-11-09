package commands

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/player"
)

// Operators are the players who may run every command, kept in a JSON file (ops.json). A player is matched by
// UUID once known, otherwise by name: an operator added by name before ever joining is tied to the UUID of the
// first player with that name. Use online mode (Xbox Live for Bedrock, Mojang for Java) so that names cannot be
// taken.
type Operators struct {
	mu   sync.Mutex
	file string
	list []operator
}

type operator struct {
	Name string `json:"name"`
	UUID string `json:"uuid,omitempty"`
}

// LoadOperators reads the operators from file, which need not exist yet.
func LoadOperators(file string) (*Operators, error) {
	o := &Operators{file: file}
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return o, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &o.list); err != nil {
		return nil, err
	}
	return o, nil
}

// Is reports whether p is an operator.
func (o *Operators) Is(p *player.Player) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	id := p.UUID().String()
	for i, op := range o.list {
		if op.UUID == id {
			return true
		}
		if op.UUID == "" && strings.EqualFold(op.Name, p.Name()) {
			o.list[i].UUID = id
			_ = o.saveLocked()
			return true
		}
	}
	return false
}

// Add makes the player called name an operator. p is the player if online, or nil.
func (o *Operators) Add(name string, p *player.Player) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	op := operator{Name: name}
	if p != nil {
		op.Name, op.UUID = p.Name(), p.UUID().String()
	}
	o.list = slices.DeleteFunc(o.list, func(e operator) bool {
		return strings.EqualFold(e.Name, op.Name) || (op.UUID != "" && e.UUID == op.UUID)
	})
	o.list = append(o.list, op)
	return o.saveLocked()
}

// Remove takes operator status from the player called name, reporting whether they had it.
func (o *Operators) Remove(name string) (bool, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	n := len(o.list)
	o.list = slices.DeleteFunc(o.list, func(e operator) bool { return strings.EqualFold(e.Name, name) })
	if len(o.list) == n {
		return false, nil
	}
	return true, o.saveLocked()
}

func (o *Operators) saveLocked() error {
	data, err := json.MarshalIndent(o.list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(o.file, append(data, '\n'), 0644)
}
