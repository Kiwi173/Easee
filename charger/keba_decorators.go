package charger

// Code generated by github.com/andig/cmd/tools/decorate.go. DO NOT EDIT.

import (
	"github.com/evcc-io/evcc/api"
)

func decorateKeba(base *Keba, meter func() (float64, error), meterEnergy func() (float64, error), chargeRater func() (float64, error), meterCurrent func() (float64, float64, float64, error)) api.Charger {
	switch {
	case chargeRater == nil && meter == nil && meterCurrent == nil && meterEnergy == nil:
		return base

	case chargeRater == nil && meter != nil && meterCurrent == nil && meterEnergy == nil:
		return &struct {
			*Keba
			api.Meter
		}{
			Keba: base,
			Meter: &decorateKebaMeterImpl{
				meter: meter,
			},
		}

	case chargeRater == nil && meter == nil && meterCurrent == nil && meterEnergy != nil:
		return &struct {
			*Keba
			api.MeterEnergy
		}{
			Keba: base,
			MeterEnergy: &decorateKebaMeterEnergyImpl{
				meterEnergy: meterEnergy,
			},
		}

	case chargeRater == nil && meter != nil && meterCurrent == nil && meterEnergy != nil:
		return &struct {
			*Keba
			api.Meter
			api.MeterEnergy
		}{
			Keba: base,
			Meter: &decorateKebaMeterImpl{
				meter: meter,
			},
			MeterEnergy: &decorateKebaMeterEnergyImpl{
				meterEnergy: meterEnergy,
			},
		}

	case chargeRater != nil && meter == nil && meterCurrent == nil && meterEnergy == nil:
		return &struct {
			*Keba
			api.ChargeRater
		}{
			Keba: base,
			ChargeRater: &decorateKebaChargeRaterImpl{
				chargeRater: chargeRater,
			},
		}

	case chargeRater != nil && meter != nil && meterCurrent == nil && meterEnergy == nil:
		return &struct {
			*Keba
			api.ChargeRater
			api.Meter
		}{
			Keba: base,
			ChargeRater: &decorateKebaChargeRaterImpl{
				chargeRater: chargeRater,
			},
			Meter: &decorateKebaMeterImpl{
				meter: meter,
			},
		}

	case chargeRater != nil && meter == nil && meterCurrent == nil && meterEnergy != nil:
		return &struct {
			*Keba
			api.ChargeRater
			api.MeterEnergy
		}{
			Keba: base,
			ChargeRater: &decorateKebaChargeRaterImpl{
				chargeRater: chargeRater,
			},
			MeterEnergy: &decorateKebaMeterEnergyImpl{
				meterEnergy: meterEnergy,
			},
		}

	case chargeRater != nil && meter != nil && meterCurrent == nil && meterEnergy != nil:
		return &struct {
			*Keba
			api.ChargeRater
			api.Meter
			api.MeterEnergy
		}{
			Keba: base,
			ChargeRater: &decorateKebaChargeRaterImpl{
				chargeRater: chargeRater,
			},
			Meter: &decorateKebaMeterImpl{
				meter: meter,
			},
			MeterEnergy: &decorateKebaMeterEnergyImpl{
				meterEnergy: meterEnergy,
			},
		}

	case chargeRater == nil && meter == nil && meterCurrent != nil && meterEnergy == nil:
		return &struct {
			*Keba
			api.MeterCurrent
		}{
			Keba: base,
			MeterCurrent: &decorateKebaMeterCurrentImpl{
				meterCurrent: meterCurrent,
			},
		}

	case chargeRater == nil && meter != nil && meterCurrent != nil && meterEnergy == nil:
		return &struct {
			*Keba
			api.Meter
			api.MeterCurrent
		}{
			Keba: base,
			Meter: &decorateKebaMeterImpl{
				meter: meter,
			},
			MeterCurrent: &decorateKebaMeterCurrentImpl{
				meterCurrent: meterCurrent,
			},
		}

	case chargeRater == nil && meter == nil && meterCurrent != nil && meterEnergy != nil:
		return &struct {
			*Keba
			api.MeterCurrent
			api.MeterEnergy
		}{
			Keba: base,
			MeterCurrent: &decorateKebaMeterCurrentImpl{
				meterCurrent: meterCurrent,
			},
			MeterEnergy: &decorateKebaMeterEnergyImpl{
				meterEnergy: meterEnergy,
			},
		}

	case chargeRater == nil && meter != nil && meterCurrent != nil && meterEnergy != nil:
		return &struct {
			*Keba
			api.Meter
			api.MeterCurrent
			api.MeterEnergy
		}{
			Keba: base,
			Meter: &decorateKebaMeterImpl{
				meter: meter,
			},
			MeterCurrent: &decorateKebaMeterCurrentImpl{
				meterCurrent: meterCurrent,
			},
			MeterEnergy: &decorateKebaMeterEnergyImpl{
				meterEnergy: meterEnergy,
			},
		}

	case chargeRater != nil && meter == nil && meterCurrent != nil && meterEnergy == nil:
		return &struct {
			*Keba
			api.ChargeRater
			api.MeterCurrent
		}{
			Keba: base,
			ChargeRater: &decorateKebaChargeRaterImpl{
				chargeRater: chargeRater,
			},
			MeterCurrent: &decorateKebaMeterCurrentImpl{
				meterCurrent: meterCurrent,
			},
		}

	case chargeRater != nil && meter != nil && meterCurrent != nil && meterEnergy == nil:
		return &struct {
			*Keba
			api.ChargeRater
			api.Meter
			api.MeterCurrent
		}{
			Keba: base,
			ChargeRater: &decorateKebaChargeRaterImpl{
				chargeRater: chargeRater,
			},
			Meter: &decorateKebaMeterImpl{
				meter: meter,
			},
			MeterCurrent: &decorateKebaMeterCurrentImpl{
				meterCurrent: meterCurrent,
			},
		}

	case chargeRater != nil && meter == nil && meterCurrent != nil && meterEnergy != nil:
		return &struct {
			*Keba
			api.ChargeRater
			api.MeterCurrent
			api.MeterEnergy
		}{
			Keba: base,
			ChargeRater: &decorateKebaChargeRaterImpl{
				chargeRater: chargeRater,
			},
			MeterCurrent: &decorateKebaMeterCurrentImpl{
				meterCurrent: meterCurrent,
			},
			MeterEnergy: &decorateKebaMeterEnergyImpl{
				meterEnergy: meterEnergy,
			},
		}

	case chargeRater != nil && meter != nil && meterCurrent != nil && meterEnergy != nil:
		return &struct {
			*Keba
			api.ChargeRater
			api.Meter
			api.MeterCurrent
			api.MeterEnergy
		}{
			Keba: base,
			ChargeRater: &decorateKebaChargeRaterImpl{
				chargeRater: chargeRater,
			},
			Meter: &decorateKebaMeterImpl{
				meter: meter,
			},
			MeterCurrent: &decorateKebaMeterCurrentImpl{
				meterCurrent: meterCurrent,
			},
			MeterEnergy: &decorateKebaMeterEnergyImpl{
				meterEnergy: meterEnergy,
			},
		}
	}

	return nil
}

type decorateKebaChargeRaterImpl struct {
	chargeRater func() (float64, error)
}

func (impl *decorateKebaChargeRaterImpl) ChargedEnergy() (float64, error) {
	return impl.chargeRater()
}

type decorateKebaMeterImpl struct {
	meter func() (float64, error)
}

func (impl *decorateKebaMeterImpl) CurrentPower() (float64, error) {
	return impl.meter()
}

type decorateKebaMeterCurrentImpl struct {
	meterCurrent func() (float64, float64, float64, error)
}

func (impl *decorateKebaMeterCurrentImpl) Currents() (float64, float64, float64, error) {
	return impl.meterCurrent()
}

type decorateKebaMeterEnergyImpl struct {
	meterEnergy func() (float64, error)
}

func (impl *decorateKebaMeterEnergyImpl) TotalEnergy() (float64, error) {
	return impl.meterEnergy()
}
