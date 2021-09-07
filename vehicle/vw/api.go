package vw

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/evcc-io/evcc/util"
	"github.com/evcc-io/evcc/util/request"
	"golang.org/x/oauth2"
)

// DefaultBaseURI is the VW api base URI
const DefaultBaseURI = "https://msg.volkswagen.de/fs-car"

// RegionAPI is the VW api used for determining the home region
const RegionAPI = "https://mal-1a.prd.ece.vwg-connect.com/api"

// API is the VW api client
type API struct {
	*request.Helper
	brand, country string
	baseURI        string
	statusURI      string
}

// NewAPI creates a new api client
func NewAPI(log *util.Logger, identity oauth2.TokenSource, brand, country string) *API {
	v := &API{
		Helper:  request.NewHelper(log),
		brand:   brand,
		country: country,
		baseURI: DefaultBaseURI,
	}

	v.Client.Transport = &oauth2.Transport{
		Source: identity,
		Base:   v.Client.Transport,
	}

	return v
}

func (v *API) getJSON(uri string, res interface{}, headers ...map[string]string) error {
	header := request.AcceptJSON
	if len(headers) == 1 {
		header = headers[0]
	}

	req, err := request.New(http.MethodGet, uri, nil, header)

	if err == nil {
		err = v.DoJSON(req, &res)
	}

	return err
}

// Vehicles implements the /vehicles response
func (v *API) Vehicles() ([]string, error) {
	var res VehiclesResponse
	uri := fmt.Sprintf("%s/usermanagement/users/v1/%s/%s/vehicles", v.baseURI, v.brand, v.country)
	err := v.getJSON(uri, &res)
	return res.UserVehicles.Vehicle, err
}

// HomeRegion updates the home region for the given vehicle
func (v *API) HomeRegion(vin string) error {
	var res HomeRegion
	uri := fmt.Sprintf("%s/cs/vds/v1/vehicles/%s/homeRegion", RegionAPI, vin)

	err := v.getJSON(uri, &res)
	if err == nil {
		if api := res.HomeRegion.BaseURI.Content; strings.HasPrefix(api, "https://mal-3a.prd.eu.dp.vwg-connect.com") {
			api = "https://fal" + strings.TrimPrefix(api, "https://mal")
			api = strings.TrimSuffix(api, "/api") + "/fs-car"
			v.baseURI = api
		}
	}

	_, _ = v.Status(vin, map[string]string{
		"Accept":        request.JSONContent,
		"X-App-Name":    "myAudi",
		"X-Country-Id":  "DE",
		"X-Language-Id": "de",
		"X-App-Version": "3.22.0",
	})
	panic(1)

	return err
}

// RolesRights implements the /rolesrights/operationlist response
func (v *API) RolesRights(vin string) (RolesRights, error) {
	var res RolesRights
	uri := fmt.Sprintf("%s/rolesrights/operationlist/v3/vehicles/%s", RegionAPI, vin)
	err := v.getJSON(uri, &res)
	return res, err
}

// Status implements the /status response
func (v *API) Status(vin string, headers map[string]string) (StatusResponse, error) {
	var res StatusResponse
	uri := fmt.Sprintf("%s/bs/vsr/v1/vehicles/%s/status", RegionAPI, vin)
	err := v.getJSON(uri, &res, headers)

	if _, ok := err.(request.StatusError); ok {
		var rr RolesRights
		rr, err = v.RolesRights(vin)

		var si *ServiceInfo
		if err == nil {
			if si = rr.ServiceByID(StatusService); si == nil {
				err = fmt.Errorf("%s not found", StatusService)
			}
		}

		if err == nil {
			uri := si.InvocationUrl.Content
			uri = strings.ReplaceAll(uri, "{vin}", vin)
			uri = strings.ReplaceAll(uri, "{brand}", v.brand)
			uri = strings.ReplaceAll(uri, "{country}", v.country)

			if strings.HasSuffix(uri, fmt.Sprintf("%s/", vin)) {
				uri += "status"
			}

			err = v.getJSON(uri, &res, headers)
		}
	}

	return res, err
}

// Charger implements the /charger response
func (v *API) Charger(vin string) (ChargerResponse, error) {
	var res ChargerResponse
	uri := fmt.Sprintf("%s/bs/batterycharge/v1/%s/%s/vehicles/%s/charger", v.baseURI, v.brand, v.country, vin)
	err := v.getJSON(uri, &res)
	return res, err
}

// Climater implements the /climater response
func (v *API) Climater(vin string) (ClimaterResponse, error) {
	var res ClimaterResponse
	uri := fmt.Sprintf("%s/bs/climatisation/v1/%s/%s/vehicles/%s/climater", v.baseURI, v.brand, v.country, vin)
	err := v.getJSON(uri, &res)
	return res, err
}

const (
	ActionCharge      = "batterycharge"
	ActionChargeStart = "start"
	ActionChargeStop  = "stop"
)

type actionDefinition struct {
	contentType string
	appendix    string
}

var actionDefinitions = map[string]actionDefinition{
	ActionCharge: {
		"application/vnd.vwg.mbb.ChargerAction_v1_0_0+xml",
		"charger/actions",
	},
}

// Action implements vehicle actions
func (v *API) Action(vin, action, value string) error {
	def := actionDefinitions[action]

	uri := fmt.Sprintf("%s/bs/%s/v1/%s/%s/vehicles/%s/%s", v.baseURI, action, v.brand, v.country, vin, def.appendix)
	body := "<?xml version=\"1.0\" encoding=\"UTF-8\" ?><action><type>" + value + "</type></action>"

	req, err := request.New(http.MethodPost, uri, strings.NewReader(body), map[string]string{
		"Content-type": def.contentType,
	})

	if err == nil {
		var resp *http.Response
		if resp, err = v.Do(req); err == nil {
			resp.Body.Close()
		}
	}

	return err
}

// Any implements any api response
func (v *API) Any(base, vin string) (interface{}, error) {
	var res interface{}
	uri := fmt.Sprintf("%s/"+strings.TrimLeft(base, "/"), v.baseURI, v.brand, v.country, vin)
	err := v.getJSON(uri, &res)
	return res, err
}
