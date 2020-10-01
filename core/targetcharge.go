package core

import "time"

const (
	utilization float64 = 0.7
	deviation           = 10 * time.Minute
)

type TargetCharge struct {
	Time time.Time
	SoC  int
	*LoadPoint
	finishAt time.Time
}

func (lp TargetCharge) Active() bool {
	inactive := lp.Time.IsZero() || lp.Time.Before(time.Now())
	return !inactive
}

func (lp TargetCharge) StartRequired() bool {
	// current/power
	current := int64(float64(lp.MaxCurrent) * utilization)
	current = clamp(current, lp.MinCurrent, lp.MaxCurrent)
	power := float64(current*lp.Phases) * Voltage

	// time
	remainingDuration := lp.socEstimator.RemainingChargeDuration(power, lp.SoC)
	lp.finishAt = time.Now().Add(remainingDuration).Round(time.Minute)

	lp.log.DEBUG.Printf("target charging remaining time: %v (finish %v at %.1f utilization)", remainingDuration, lp.finishAt, utilization)

	return lp.finishAt.After(lp.Time)
}

func (lp TargetCharge) Handle() error {
	current := lp.handler.TargetCurrent()

	switch {
	case lp.finishAt.Before(lp.Time.Add(-deviation)):
		current -= 1
		lp.log.DEBUG.Printf("target charging: slowdown")

	case lp.finishAt.After(lp.Time.Add(deviation)):
		current += 1
		lp.log.DEBUG.Printf("target charging: speedup")
	}

	current = clamp(current, lp.MinCurrent, lp.MaxCurrent)

	return lp.handler.Ramp(current)
}
