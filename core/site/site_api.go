package site

import (
	"errors"

	"github.com/andig/evcc/api"
	"github.com/andig/evcc/core/loadpoint"
)

// API is the external site API
type API interface {
	Healthy() bool
	LoadPoints() []loadpoint.API
	GetConfig() Config
	GetPrioritySoC() float64
	SetPrioritySoC(float64) error
}

// GetConfig returns the site configuration
func (s *Site) GetConfig() Config {
	s.Lock()
	defer s.Unlock()
	return s.Config
}

// GetPrioritySoC returns the PrioritySoC
func (s *Site) GetPrioritySoC() float64 {
	s.Lock()
	defer s.Unlock()
	return s.PrioritySoC
}

// SetPrioritySoC sets the PrioritySoC
func (s *Site) SetPrioritySoC(soc float64) error {
	s.Lock()
	defer s.Unlock()

	if _, ok := s.batteryMeter.(api.Battery); !ok {
		return errors.New("battery not configured")
	}

	s.PrioritySoC = soc
	s.publish("prioritySoC", s.PrioritySoC)

	return nil
}
