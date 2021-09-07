package porsche

import (
	"fmt"
	"net/http"
	"time"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/provider"
	"github.com/evcc-io/evcc/util/request"
	"golang.org/x/oauth2"
)

type CapabilitiesResponse struct {
	DisplayParkingBrake      bool
	NeedsSPIN                bool
	HasRDK                   bool
	EngineType               string
	CarModel                 string
	OnlineRemoteUpdateStatus struct {
		EditableByUser bool
		Active         bool
	}
	HeatingCapabilities struct {
		FrontSeatHeatingAvailable bool
		RearSeatHeatingAvailable  bool
	}
	SteeringWheelPosition string
	HasHonkAndFlash       bool
}

type EmobilityResponse struct {
	BatteryChargeStatus struct {
		ChargeRate struct {
			Unit             string
			Value            float64
			ValueInKmPerHour int64
		}
		ChargingInDCMode                            bool
		ChargingMode                                string
		ChargingPower                               float64
		ChargingReason                              string
		ChargingState                               string
		ChargingTargetDateTime                      string
		ExternalPowerSupplyState                    string
		PlugState                                   string
		RemainingChargeTimeUntil100PercentInMinutes int64
		StateOfChargeInPercentage                   int64
		RemainingERange                             struct {
			OriginalUnit      string
			OriginalValue     int64
			Unit              string
			Value             int64
			ValueInKilometers int64
		}
	}
	ChargingStatus string
	DirectCharge   struct {
		Disabled bool
		IsActive bool
	}
	DirectClimatisation struct {
		ClimatisationState         string
		RemainingClimatisationTime int64
	}
}

// EMobilityProvider is an api.Vehicle implementation for Porsche Taycan cars
type EMobilityProvider struct {
	log api.Logger
	*request.Helper
	token    oauth2.Token
	identity *Identity
	carModel string
	statusG  func() (interface{}, error)
}

// NewEMobilityProvider creates a new vehicle
func NewEMobilityProvider(log api.Logger, identity *Identity, token oauth2.Token, vin string, cache time.Duration) *EMobilityProvider {
	impl := &EMobilityProvider{
		log:      log,
		token:    token,
		Helper:   request.NewHelper(log),
		identity: identity,
	}

	impl.statusG = provider.NewCached(func() (interface{}, error) {
		return impl.status(vin)
	}, cache).InterfaceGetter()

	return impl
}

func (v *EMobilityProvider) request(uri string) (*http.Request, error) {
	if v.token.AccessToken == "" || time.Since(v.token.Expiry) > 0 {
		accessTokens, err := v.identity.Login()
		if err != nil {
			return nil, err
		}
		v.token = accessTokens.EmobilityToken
	}

	req, err := request.New(http.MethodGet, uri, nil, map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", v.token.AccessToken),
		"apikey":        EmobilityClientID,
	})

	return req, err
}

// Status implements the vehicle status response
func (v *EMobilityProvider) status(vin string) (interface{}, error) {
	if v.carModel == "" {
		uri := fmt.Sprintf("https://api.porsche.com/service-vehicle/vcs/capabilities/%s", vin)
		req, err := v.request(uri)
		if err != nil {
			return 0, err
		}

		req.Header.Set("x-vrs-url-country", "de")
		req.Header.Set("x-vrs-url-language", "de_DE")
		var cr CapabilitiesResponse
		err = v.DoJSON(req, &cr)
		if err != nil {
			return 0, err
		}
		v.carModel = cr.CarModel
	}

	uri := fmt.Sprintf("https://api.porsche.com/service-vehicle/de/de_DE/e-mobility/%s/%s?timezone=Europe/Berlin", v.carModel, vin)
	req, err := v.request(uri)
	if err != nil {
		return 0, err
	}

	var pr EmobilityResponse
	err = v.DoJSON(req, &pr)

	return pr, err
}

var _ api.Battery = (*Provider)(nil)

// SoC implements the api.Vehicle interface
func (v *EMobilityProvider) SoC() (float64, error) {
	res, err := v.statusG()
	if res, ok := res.(EmobilityResponse); err == nil && ok {
		return float64(res.BatteryChargeStatus.StateOfChargeInPercentage), nil
	}

	return 0, err
}

var _ api.VehicleRange = (*Provider)(nil)

// Range implements the api.VehicleRange interface
func (v *EMobilityProvider) Range() (int64, error) {
	res, err := v.statusG()
	if res, ok := res.(EmobilityResponse); err == nil && ok {
		return int64(res.BatteryChargeStatus.RemainingERange.ValueInKilometers), nil
	}

	return 0, err
}

var _ api.VehicleFinishTimer = (*EMobilityProvider)(nil)

// FinishTime implements the api.VehicleFinishTimer interface
func (v *EMobilityProvider) FinishTime() (time.Time, error) {
	res, err := v.statusG()
	if res, ok := res.(*EmobilityResponse); err == nil && ok {
		t := time.Now()
		return t.Add(time.Duration(res.BatteryChargeStatus.RemainingChargeTimeUntil100PercentInMinutes) * time.Minute), err
	}

	return time.Time{}, err
}

var _ api.ChargeState = (*EMobilityProvider)(nil)

// Status implements the api.ChargeState interface
func (v *EMobilityProvider) Status() (api.ChargeStatus, error) {
	res, err := v.statusG()
	if res, ok := res.(EmobilityResponse); err == nil && ok {
		switch res.BatteryChargeStatus.PlugState {
		case "DISCONNECTED":
			return api.StatusA, nil
		case "CONNECTED":
			// ignore if the car is connected to a DC charging station
			if res.BatteryChargeStatus.ChargingInDCMode {
				return api.StatusA, nil
			}
			switch res.BatteryChargeStatus.ChargingState {
			case "OFF", "COMPLETED":
				return api.StatusB, nil
			case "ON":
				return api.StatusC, nil
			}
		}
	}

	return api.StatusNone, err
}

var _ api.VehicleClimater = (*EMobilityProvider)(nil)

// Climater implements the api.VehicleClimater interface
func (v *EMobilityProvider) Climater() (active bool, outsideTemp float64, targetTemp float64, err error) {
	res, err := v.statusG()
	if res, ok := res.(EmobilityResponse); err == nil && ok {
		switch res.DirectClimatisation.ClimatisationState {
		case "OFF":
			return false, 0, 0, nil
		case "ON":
			return true, 0, 0, nil
		}
	}

	return active, outsideTemp, targetTemp, err
}
