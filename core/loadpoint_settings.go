package core

import "time"

type RuntimeSettings struct {
	TargetSoC int       `json:"targetSoc"`
	FinishAt  time.Time `json:"finishAt"`
}
