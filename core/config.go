package core

import (
	"fmt"

	"github.com/andig/evcc/api"
	"github.com/avast/retry-go"
)

var (

	// Voltage global value
	Voltage float64 = 230

	// RetryOptions ist the default options set for retryable operations
	RetryOptions = []retry.Option{retry.Attempts(3), retry.LastErrorOnly(true)}

	Status   = map[bool]string{false: "disable", true: "enable"}
	Presence = map[bool]string{false: "—", true: "✓"}
)

// ConfigProvider gives access to configuration repository
type ConfigProvider interface {
	Meter(string) api.Meter
	Charger(string) api.Charger
	Vehicle(string) api.Vehicle
}

func MeterCapabilities(name string, meter interface{}) string {
	_, power := meter.(api.Meter)
	_, energy := meter.(api.MeterEnergy)
	_, currents := meter.(api.MeterCurrent)

	name += ":"
	return fmt.Sprintf("    %-8s power %s energy %s currents %s",
		name,
		Presence[power],
		Presence[energy],
		Presence[currents],
	)
}
