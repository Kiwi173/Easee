package core

import "time"

const (
	utilization float64 = 0.7
	deviation           = 5 * time.Minute
)

type TargetCharge struct {
	*LoadPoint
	Time    time.Time
	SoC     int
	current int64
}

func (lp TargetCharge) Active() bool {
	inactive := lp.Time.IsZero() || lp.Time.Before(time.Now())
	return !inactive
}

func (lp TargetCharge) StartRequired() bool {
	// remainingEnergy := lp.socEstimator.RemainingChargeEnergy(lp.TargetSoC) / utilization
	// lp.log.DEBUG.Printf("target charging remaining energy: %.1f", remainingEnergy)

	// current/power
	lp.current = int64(float64(lp.MaxCurrent) * utilization)
	lp.current = clamp(lp.current, lp.MinCurrent, lp.MaxCurrent)
	power := float64(lp.current*lp.Phases) * Voltage

	// time
	remainingDuration := lp.socEstimator.RemainingChargeDuration(power, lp.SoC)
	lp.finishAt = time.Now().Add(remainingDuration).Round(time.Minute)

	lp.log.DEBUG.Printf("target charging remaining time: %v (finish %v at %.1f utilization)", remainingDuration, lp.finishAt, utilization)

	return finishAt.After(lp.Time)
}

func (lp TargetCharge) Handle() error {
	switch {
	case lp.finishAt.Before(lp.Time.Add(-deviation)):
		lp.current = lp.current - 1
	case lp.finishAt.After(lp.Time.Add(deviation)):
		lp.current = lp.current + 1
	}

	lp.current = clamp(lp.current, lp.MinCurrent, lp.MaxCurrent)

	return lp.handler.Ramp(lp.current)
}
