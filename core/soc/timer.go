package soc

import (
	"math"
	"time"

	"github.com/andig/evcc/util"
)

const (
	deviation = 30 * time.Minute
)

// Adapter provides the required methods for interacting with the loadpoint
type Adapter interface {
	Publish(key string, val interface{})
	SocEstimator() *Estimator
	ActivePhases() int64
	Voltage() float64
}

// Timer is the target charging handler
type Timer struct {
	Adapter
	log            *util.Logger
	maxCurrent     float64
	current        float64
	SoC            int
	Time           time.Time
	finishAt       time.Time
	chargeRequired bool
}

// NewTimer creates a Timer
func NewTimer(log *util.Logger, adapter Adapter, maxCurrent float64) *Timer {
	lp := &Timer{
		log:        log,
		Adapter:    adapter,
		maxCurrent: maxCurrent,
	}

	return lp
}

// Reset resets the target charging request
func (lp *Timer) Reset() {
	if lp == nil {
		return
	}

	lp.current = float64(lp.maxCurrent)
	lp.Time = time.Time{}
	lp.SoC = 0
}

// StartRequired calculates remaining charge duration and returns true if charge start is required to achieve target soc in time
func (lp *Timer) StartRequired() bool {
	if lp == nil {
		return false
	}

	se := lp.SocEstimator()
	if !lp.active() || se == nil {
		lp.log.TRACE.Printf("target charging: not active")
		return false
	}

	power := float64(lp.ActivePhases()) * lp.maxCurrent * lp.Voltage()

	// time
	remainingDuration := se.RemainingChargeDuration(power, lp.SoC)
	lp.finishAt = time.Now().Add(remainingDuration).Round(time.Minute)
	lp.log.DEBUG.Printf("target charging active for %v: projected %v (%v remaining)", lp.Time, lp.finishAt, remainingDuration.Round(time.Minute))

	lp.chargeRequired = lp.finishAt.After(lp.Time)
	lp.Publish("timerActive", lp.chargeRequired)

	return lp.chargeRequired
}

// active returns true if there is an active target charging request
func (lp *Timer) active() bool {
	inactive := lp.Time.IsZero() || lp.Time.Before(time.Now())
	lp.Publish("timerSet", !inactive)

	// reset active
	if inactive && lp.chargeRequired {
		lp.log.TRACE.Printf("target charging: deactivating") // TODO remove
		lp.chargeRequired = false
		lp.Publish("timerActive", lp.chargeRequired)
	}

	return !inactive
}

// Handle adjusts current up/down to achieve desired target time taking.
func (lp *Timer) Handle() float64 {
	action := "steady"

	switch {
	case lp.finishAt.Before(lp.Time.Add(-deviation)):
		lp.current--
		action = "slowdown"

	case lp.finishAt.After(lp.Time):
		lp.current++
		action = "speedup"
	}

	lp.current = math.Max(math.Min(lp.current, float64(lp.maxCurrent)), 0)
	lp.log.DEBUG.Printf("target charging: %s (%.3gA)", action, lp.current)

	return lp.current
}
