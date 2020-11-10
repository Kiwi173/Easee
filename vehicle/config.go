package vehicle

import (
	"fmt"
	"strings"

	"github.com/andig/evcc/api"
)

type vehicleRegistry map[string]func(map[string]interface{}) (api.Vehicle, error)

func (r vehicleRegistry) Add(name string, factory func(map[string]interface{}) (api.Vehicle, error)) {
	if _, exists := r[name]; exists {
		panic(fmt.Sprintf("cannot register duplicate vehicle type: %s", name))
	}
	r[name] = factory
}

func (r vehicleRegistry) Get(name string) (func(map[string]interface{}) (api.Vehicle, error), error) {
	factory, exists := r[name]
	if !exists {
		return nil, fmt.Errorf("vehicle type not registered: %s", name)
	}
	return factory, nil
}

var registry vehicleRegistry = make(map[string]func(map[string]interface{}) (api.Vehicle, error))

// Registry returns the module's registry singleton
func Registry() vehicleRegistry {
	return registry
}

// NewFromConfig creates vehicle from configuration
func NewFromConfig(typ string, other map[string]interface{}) (v api.Vehicle, err error) {
	factory, err := registry.Get(strings.ToLower(typ))
	if err == nil {
		if v, err = factory(other); err != nil {
			err = fmt.Errorf("cannot create %s vehicle: %w", typ, err)
		}
	} else {
		err = fmt.Errorf("invalid vehicle type: %s", typ)
	}

	return
}
