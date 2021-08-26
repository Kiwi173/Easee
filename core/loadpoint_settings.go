package core

import "time"

type RuntimeSettings struct {
	TargetSoC int       `json:"targetSoc"`
	FinishAt  time.Time `json:"finishAt"`
}

func (lp *LoadPoint) saveSettings() {
	if err := settings.Save(lp, lp.rt); err != nil {
		lp.log.ERROR.Printf("save settings: %v", err)
	}
}

func (lp *LoadPoint) loadSettings() {
	rt, err := settings.Load(lp)
	if err != nil {
		lp.log.ERROR.Printf("load settings: %v", err)
	}

	lp.rt = rt

	if rt.FinishAt.After(lp.clock.Now()) {
		lp.SetTargetCharge(rt.FinishAt, rt.TargetSoC)
	} else if rt.TargetSoC > 0 {
		lp.SetTargetSoC(rt.TargetSoC)
	}
}
