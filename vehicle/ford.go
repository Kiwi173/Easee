package vehicle

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/provider"
	"github.com/evcc-io/evcc/util"
	"github.com/evcc-io/evcc/util/oauth"
	"github.com/evcc-io/evcc/util/request"
	"golang.org/x/oauth2"
)

const (
	fordAuth           = "https://fcis.ice.ibmcloud.com"
	fordAPI            = "https://usapi.cv.ford.com"
	fordVehicleList    = "https://api.mps.ford.com/api/users/vehicles"
	fordStatusExpiry   = 5 * time.Minute       // if returned status value is older, evcc will init refresh
	fordRefreshTimeout = time.Minute           // timeout to get status after refresh
	fordTimeFormat     = "01-02-2006 15:04:05" // time format used by Ford API, time is in UTC
)

// Ford is an api.Vehicle implementation for Ford cars
type Ford struct {
	*embed
	*request.Helper
	log                 *util.Logger
	user, password, vin string
	tokenSource         oauth2.TokenSource
	statusG             func() (interface{}, error)
	refreshId           string
	refreshTime         time.Time
}

func init() {
	registry.Add("ford", NewFordFromConfig)
}

// NewFordFromConfig creates a new vehicle
func NewFordFromConfig(other map[string]interface{}) (api.Vehicle, error) {
	cc := struct {
		embed               `mapstructure:",squash"`
		User, Password, VIN string
		Cache               time.Duration
	}{
		Cache: interval,
	}

	if err := util.DecodeOther(other, &cc); err != nil {
		return nil, err
	}

	if cc.User == "" || cc.Password == "" {
		return nil, errors.New("missing credentials")
	}

	log := util.NewLogger("ford")

	v := &Ford{
		embed:    &cc.embed,
		Helper:   request.NewHelper(log),
		log:      log,
		user:     cc.User,
		password: cc.Password,
		vin:      strings.ToUpper(cc.VIN),
	}

	token, err := v.login()
	if err == nil {
		v.tokenSource = oauth.RefreshTokenSource((*oauth2.Token)(&token), v)
	}

	v.statusG = provider.NewCached(func() (interface{}, error) {
		return v.status()
	}, cc.Cache).InterfaceGetter()

	if err == nil && cc.VIN == "" {
		v.vin, err = findVehicle(v.vehicles())
		if err == nil {
			log.Debugf("found vehicle: %v", v.vin)
		}
	}

	return v, err
}

// login authenticates with username/password to get new token
func (v *Ford) login() (oauth.Token, error) {
	data := url.Values{
		"client_id":  []string{"9fb503e0-715b-47e8-adfd-ad4b7770f73b"},
		"grant_type": []string{"password"},
		"username":   []string{v.user},
		"password":   []string{v.password},
	}

	uri := fordAuth + "/v1.0/endpoint/default/token"
	req, err := request.New(http.MethodPost, uri, strings.NewReader(data.Encode()), request.URLEncoding)

	var res oauth.Token
	if err == nil {
		err = v.DoJSON(req, &res)
	}

	return res, err
}

// Refresh implements oauth.TokenRefresher
func (v *Ford) RefreshToken(token *oauth2.Token) (*oauth2.Token, error) {
	data := url.Values{
		"client_id":     []string{"9fb503e0-715b-47e8-adfd-ad4b7770f73b"},
		"grant_type":    []string{"refresh_token"},
		"refresh_token": []string{token.RefreshToken},
	}

	uri := fordAuth + "/v1.0/endpoint/default/token"
	req, err := request.New(http.MethodPost, uri, strings.NewReader(data.Encode()), request.URLEncoding)

	var res oauth.Token
	if err == nil {
		err = v.DoJSON(req, &res)
	}

	if err != nil {
		res, err = v.login()
	}

	return (*oauth2.Token)(&res), err
}

// request is a helper to send API requests, sets header the Ford API expects
func (v *Ford) request(method, uri string) (*http.Request, error) {
	token, err := v.tokenSource.Token()

	var req *http.Request
	if err == nil {
		req, err = request.New(method, uri, nil, map[string]string{
			"Content-type":   "application/json",
			"Application-Id": "71A3AD0A-CF46-4CCF-B473-FC7FE5BC4592",
			"Auth-Token":     token.AccessToken,
		})
	}

	return req, err
}

// fordVehicleStatus holds the relevant data extracted from JSON that the server sends
// on vehicle status request
type fordVehicleStatus struct {
	VehicleStatus struct {
		BatteryFillLevel struct {
			Value     float64
			Timestamp string
		}
		ElVehDTE struct {
			Value     float64
			Timestamp string
		}
		ChargingStatus struct {
			Value     string
			Timestamp string
		}
		PlugStatus struct {
			Value     int
			Timestamp string
		}
		LastRefresh string
	}
	Status int
}

// vehicles returns the list of user vehicles
func (v *Ford) vehicles() ([]string, error) {
	var resp struct {
		Vehicles struct {
			Values []struct {
				VIN string
			} `json:"$values"`
		}
	}

	req, err := v.request(http.MethodGet, fordVehicleList)
	if err == nil {
		err = v.DoJSON(req, &resp)
	}

	var vehicles []string
	if err == nil {
		for _, v := range resp.Vehicles.Values {
			vehicles = append(vehicles, v.VIN)
		}
	}

	return vehicles, err
}

// status performs a /status request to the Ford API and triggers a refresh if
// the received status is too old
func (v *Ford) status() (res fordVehicleStatus, err error) {
	// follow up requested refresh
	if v.refreshId != "" {
		return v.refreshResult()
	}

	// otherwise start normal workflow
	uri := fmt.Sprintf("%s/api/vehicles/v3/%s/status", fordAPI, v.vin)
	req, err := v.request(http.MethodGet, uri)
	if err == nil {
		err = v.DoJSON(req, &res)
	}

	if err == nil {
		var lastUpdate time.Time
		lastUpdate, err = time.Parse(fordTimeFormat, res.VehicleStatus.LastRefresh)

		if elapsed := time.Since(lastUpdate); err == nil && elapsed > fordStatusExpiry {
			v.log.Debugf("vehicle status is outdated (age %v > %v), requesting refresh", elapsed, fordStatusExpiry)

			if err = v.refreshRequest(); err == nil {
				err = api.ErrMustRetry
			}
		}
	}

	return res, err
}

// refreshResult triggers an update if not already in progress, otherwise gets result
func (v *Ford) refreshResult() (res fordVehicleStatus, err error) {
	uri := fmt.Sprintf("%s/api/vehicles/v3/%s/statusrefresh/%s", fordAPI, v.vin, v.refreshId)

	var req *http.Request
	if req, err = v.request(http.MethodGet, uri); err == nil {
		err = v.DoJSON(req, &res)
	}

	// update successful and completed
	if err == nil && res.Status == 200 {
		v.refreshId = ""
		return res, nil
	}

	// update still in progress, keep retrying
	if time.Since(v.refreshTime) < fordRefreshTimeout {
		return res, api.ErrMustRetry
	}

	// give up
	v.refreshId = ""
	if err == nil {
		err = api.ErrTimeout
	}

	return res, err
}

// refreshRequest requests status refresh tracked by commandId
func (v *Ford) refreshRequest() error {
	var resp struct {
		CommandId string
	}

	uri := fmt.Sprintf("%s/api/vehicles/v2/%s/status", fordAPI, v.vin)
	req, err := v.request(http.MethodPut, uri)
	if err == nil {
		err = v.DoJSON(req, &resp)
	}

	if err == nil {
		v.refreshId = resp.CommandId
		v.refreshTime = time.Now()

		if resp.CommandId == "" {
			err = errors.New("refresh failed")
		}
	}

	return err
}

var _ api.Battery = (*Ford)(nil)

// SoC implements the api.Battery interface
func (v *Ford) SoC() (float64, error) {
	res, err := v.statusG()
	if res, ok := res.(fordVehicleStatus); err == nil && ok {
		return float64(res.VehicleStatus.BatteryFillLevel.Value), nil
	}

	return 0, err
}

var _ api.VehicleRange = (*Ford)(nil)

// Range implements the api.VehicleRange interface
func (v *Ford) Range() (int64, error) {
	res, err := v.statusG()
	if res, ok := res.(fordVehicleStatus); err == nil && ok {
		return int64(res.VehicleStatus.ElVehDTE.Value), nil
	}

	return 0, err
}

var _ api.ChargeState = (*Ford)(nil)

// Status implements the api.ChargeState interface
func (v *Ford) Status() (api.ChargeStatus, error) {
	status := api.StatusA // disconnected

	res, err := v.statusG()
	if res, ok := res.(fordVehicleStatus); err == nil && ok {
		if res.VehicleStatus.PlugStatus.Value == 1 {
			status = api.StatusB // connected, not charging
		}
		if res.VehicleStatus.ChargingStatus.Value == "ChargingAC" {
			status = api.StatusC // charging
		}
	}

	return status, err
}
