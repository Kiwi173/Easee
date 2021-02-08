package loadpoint

import (
	"github.com/andig/evcc/core"
)

// powerToCurrent is a helper function to convert power to per-phase current
func powerToCurrent(power float64, phases int64) float64 {
	return power / (float64(phases) * core.Voltage)
}

// consumedPower estimates how much power the charger might have consumed given it was the only load
// func consumedPower(pv, battery, grid float64) float64 {
// 	return math.Abs(pv) + battery + grid
// }
