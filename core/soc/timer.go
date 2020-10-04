package soc

import "time"

const (
	utilization float64 = 0.6
	deviation           = 30 * time.Minute
)

// Timer is the target charging handler
type Timer struct {
	// *LoadPoint
	SoC            int
	Time           time.Time
	finishAt       time.Time
	chargeRequired bool
}

// NewTimer creates target charging timer/controller
func NewTimer() *Timer {
	lp := &Timer{}

	return lp
}

// core/soc/timer.go:28:11: lp.socEstimator undefined (type *Timer has no field or method socEstimator)
// core/soc/timer.go:46:4: lp.publish undefined (type *Timer has no field or method publish)
// core/soc/timer.go:51:5: lp.publish undefined (type *Timer has no field or method publish)
// core/soc/timer.go:63:15: lp.effectiveCurrent undefined (type *Timer has no field or method effectiveCurrent)
// core/soc/timer.go:67:29: lp.MaxCurrent undefined (type *Timer has no field or method MaxCurrent)
// core/soc/timer.go:68:13: undefined: clamp
// core/soc/timer.go:68:30: lp.MinCurrent undefined (type *Timer has no field or method MinCurrent)
// core/soc/timer.go:68:45: lp.MaxCurrent undefined (type *Timer has no field or method MaxCurrent)
// core/soc/timer.go:71:29: lp.Phases undefined (type *Timer has no field or method Phases)
// core/soc/timer.go:71:40: undefined: Voltage

// Supported returns true if target charging is possible, i.e. the vehicle soc can be determined
func (lp *Timer) Supported() bool {
	return lp.socEstimator != nil
}

// Reset resets the target charging request
func (lp *Timer) Reset() {
	if lp != nil {
		lp.Time = time.Time{}
		lp.SoC = 0
	}
}

// active returns true if there is an active target charging request
func (lp *Timer) active() bool {
	if lp == nil {
		return false
	}

	inactive := lp.Time.IsZero() || lp.Time.Before(time.Now())
	lp.publish("TimerSet", !inactive)

	// reset active
	if inactive && lp.chargeRequired {
		lp.chargeRequired = false
		lp.publish("TimerActive", lp.chargeRequired)
	}

	return !inactive
}

// StartRequired calculates remaining charge duration and returns true if charge start is required to achieve target soc in time
func (lp *Timer) StartRequired() bool {
	if !lp.active() {
		return false
	}

	current := lp.effectiveCurrent()

	// use start current for calculation if currently not charging
	if current == 0 {
		current = int64(float64(lp.MaxCurrent) * utilization)
		current = clamp(current, lp.MinCurrent, lp.MaxCurrent)
	}

	power := float64(current*lp.Phases) * Voltage

	// time
	remainingDuration := lp.socEstimator.RemainingChargeDuration(power, lp.SoC)
	lp.finishAt = time.Now().Add(remainingDuration).Round(time.Minute)
	lp.log.DEBUG.Printf("target charging active for %v: projected %v (%v remaining)", lp.Time, lp.finishAt, remainingDuration.Round(time.Minute))

	lp.chargeRequired = lp.finishAt.After(lp.Time)
	lp.publish("TimerActive", lp.chargeRequired)

	return lp.chargeRequired
}

// Handle adjusts current up/down to achieve desired target time taking.
// PV mode target current into consideration to ensure maximum PV usage.
func (lp *Timer) Handle(pvCurrent int64) error {
	current := lp.handler.TargetCurrent()

	switch {
	case lp.finishAt.Before(lp.Time.Add(-deviation)):
		current--
		lp.log.DEBUG.Printf("target charging: slowdown")

	case lp.finishAt.After(lp.Time):
		current++
		lp.log.DEBUG.Printf("target charging: speedup")
	}

	// use higher-charging pv current if available
	if current < pvCurrent {
		current = pvCurrent
	}

	current = clamp(current, lp.MinCurrent, lp.MaxCurrent)

	return lp.handler.Ramp(current)
}
