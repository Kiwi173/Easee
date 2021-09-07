package core

import (
	"time"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/core/loadpoint"
	"github.com/evcc-io/evcc/core/wrapper"
)

var _ loadpoint.API = (*LoadPoint)(nil)

// GetStatus returns the charging status
func (lp *LoadPoint) GetStatus() api.ChargeStatus {
	lp.Lock()
	defer lp.Unlock()
	return lp.status
}

// GetMode returns loadpoint charge mode
func (lp *LoadPoint) GetMode() api.ChargeMode {
	lp.Lock()
	defer lp.Unlock()
	return lp.Mode
}

// SetMode sets loadpoint charge mode
func (lp *LoadPoint) SetMode(mode api.ChargeMode) {
	lp.Lock()
	defer lp.Unlock()

	lp.log.Infof("set charge mode: %s", string(mode))

	// apply immediately
	if lp.Mode != mode {
		lp.Mode = mode
		lp.publish("mode", mode)

		// immediately allow pv mode activity
		lp.elapsePVTimer()

		lp.requestUpdate()
	}
}

// GetTargetSoC returns loadpoint charge target soc
func (lp *LoadPoint) GetTargetSoC() int {
	lp.Lock()
	defer lp.Unlock()
	return lp.SoC.Target
}

// SetTargetSoC sets loadpoint charge target soc
func (lp *LoadPoint) SetTargetSoC(soc int) error {
	if lp.vehicle == nil {
		return api.ErrNotAvailable
	}

	lp.Lock()
	defer lp.Unlock()

	lp.log.Infoln("set target soc:", soc)

	// apply immediately
	if lp.SoC.Target != soc {
		lp.SoC.Target = soc
		lp.publish("targetSoC", soc)
		lp.requestUpdate()
	}

	return nil
}

// GetMinSoC returns loadpoint charge minimum soc
func (lp *LoadPoint) GetMinSoC() int {
	lp.Lock()
	defer lp.Unlock()
	return lp.SoC.Min
}

// SetMinSoC sets loadpoint charge minimum soc
func (lp *LoadPoint) SetMinSoC(soc int) error {
	if lp.vehicle == nil {
		return api.ErrNotAvailable
	}

	lp.Lock()
	defer lp.Unlock()

	lp.log.Infoln("set min soc:", soc)

	// apply immediately
	if lp.SoC.Min != soc {
		lp.SoC.Min = soc
		lp.publish("minSoC", soc)
		lp.requestUpdate()
	}

	return nil
}

// GetPhases returns loadpoint enabled phases
func (lp *LoadPoint) GetPhases() int {
	lp.Lock()
	defer lp.Unlock()

	return int(lp.Phases)
}

// SetPhases sets loadpoint enabled phases
func (lp *LoadPoint) SetPhases(phases int) error {
	return lp.scalePhases(phases)
}

// SetTargetCharge sets loadpoint charge targetSoC
func (lp *LoadPoint) SetTargetCharge(finishAt time.Time, targetSoC int) {
	lp.Lock()
	defer lp.Unlock()

	lp.log.Infof("set target charge: %d @ %v", targetSoC, finishAt)

	// apply immediately
	// TODO check reset of targetSoC
	lp.publish("targetTime", finishAt)
	lp.publish("targetSoC", targetSoC)

	lp.socTimer.Time = finishAt
	lp.socTimer.SoC = targetSoC

	lp.requestUpdate()
}

// RemoteControl sets remote status demand
func (lp *LoadPoint) RemoteControl(source string, demand loadpoint.RemoteDemand) {
	lp.Lock()
	defer lp.Unlock()

	lp.log.Infoln("remote demand:", demand)

	// apply immediately
	if lp.remoteDemand != demand {
		lp.remoteDemand = demand

		lp.publish("remoteDisabled", demand)
		lp.publish("remoteDisabledSource", source)

		lp.requestUpdate()
	}
}

// HasChargeMeter determines if a physical charge meter is attached
func (lp *LoadPoint) HasChargeMeter() bool {
	_, isWrapped := lp.chargeMeter.(*wrapper.ChargeMeter)
	return lp.chargeMeter != nil && !isWrapped
}

// GetChargePower returns the current charge power
func (lp *LoadPoint) GetChargePower() float64 {
	lp.Lock()
	defer lp.Unlock()
	return lp.chargePower
}

// GetMinCurrent returns the min loadpoint current
func (lp *LoadPoint) GetMinCurrent() float64 {
	lp.Lock()
	defer lp.Unlock()
	return lp.MinCurrent
}

// SetMinCurrent returns the min loadpoint current
func (lp *LoadPoint) SetMinCurrent(current float64) {
	lp.Lock()
	defer lp.Unlock()

	if current != lp.MinCurrent {
		lp.MinCurrent = current
		lp.publish("minCurrent", lp.MinCurrent)
	}
}

// GetMaxCurrent returns the max loadpoint current
func (lp *LoadPoint) GetMaxCurrent() float64 {
	lp.Lock()
	defer lp.Unlock()
	return lp.MaxCurrent
}

// SetMaxCurrent returns the max loadpoint current
func (lp *LoadPoint) SetMaxCurrent(current float64) {
	lp.Lock()
	defer lp.Unlock()

	if current != lp.MaxCurrent {
		lp.MaxCurrent = current
		lp.publish("maxCurrent", lp.MaxCurrent)
	}
}

// GetMinPower returns the min loadpoint power for a single phase
func (lp *LoadPoint) GetMinPower() float64 {
	return Voltage * lp.GetMinCurrent()
}

// GetMaxPower returns the max loadpoint power taking active phases into account
func (lp *LoadPoint) GetMaxPower() float64 {
	return Voltage * lp.GetMaxCurrent() * float64(lp.GetPhases())
}

// setRemainingDuration sets the estimated remaining charging duration
func (lp *LoadPoint) setRemainingDuration(chargeRemainingDuration time.Duration) {
	lp.Lock()
	defer lp.Unlock()

	if lp.chargeRemainingDuration != chargeRemainingDuration {
		lp.chargeRemainingDuration = chargeRemainingDuration
		lp.publish("chargeRemainingDuration", chargeRemainingDuration)
	}
}

// GetRemainingDuration is the estimated remaining charging duration
func (lp *LoadPoint) GetRemainingDuration() time.Duration {
	lp.Lock()
	defer lp.Unlock()
	return lp.chargeRemainingDuration
}

// setRemainingEnergy sets the remaining charge energy in Wh
func (lp *LoadPoint) setRemainingEnergy(chargeRemainingEnergy float64) {
	lp.Lock()
	defer lp.Unlock()

	if lp.chargeRemainingEnergy != chargeRemainingEnergy {
		lp.chargeRemainingEnergy = chargeRemainingEnergy
		lp.publish("chargeRemainingEnergy", chargeRemainingEnergy)
	}
}

// GetRemainingEnergy is the remaining charge energy in Wh
func (lp *LoadPoint) GetRemainingEnergy() float64 {
	lp.Lock()
	defer lp.Unlock()
	return lp.chargeRemainingEnergy
}
