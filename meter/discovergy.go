package meter

import (
	"fmt"
	"net/http"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/provider"
	"github.com/evcc-io/evcc/util"
	"github.com/evcc-io/evcc/util/request"
	"github.com/thoas/go-funk"
)

const discovergyAPI = "https://api.discovergy.com/public/v1"

func init() {
	registry.Add("discovergy", NewDiscovergyFromConfig)
}

type discovergyMeter struct {
	MeterID          string `json:"meterId"`
	SerialNumber     string `json:"serialNumber"`
	FullSerialNumber string `json:"fullSerialNumber"`
}

// NewDiscovergyFromConfig creates a new configurable meter
func NewDiscovergyFromConfig(other map[string]interface{}) (api.Meter, error) {
	cc := struct {
		User     string
		Password string
		Meter    string
		Scale    float64
	}{
		Scale: 1,
	}

	if err := util.DecodeOther(other, &cc); err != nil {
		return nil, err
	}

	log := util.NewLogger("discgy")

	headers := make(map[string]string)
	if err := provider.AuthHeaders(log, provider.Auth{
		Type:     "Basic",
		User:     cc.User,
		Password: cc.Password,
	}, headers); err != nil {
		return nil, err
	}

	req, err := request.New(http.MethodGet, fmt.Sprintf("%s/meters", discovergyAPI), nil, headers)
	if err != nil {
		return nil, err
	}

	var meters []discovergyMeter
	if err := request.NewHelper(log).DoJSON(req, &meters); err != nil {
		return nil, err
	}

	var meterID string
	if cc.Meter != "" {
		for _, m := range meters {
			if matchesIdentifier(cc.Meter, m) {
				meterID = m.MeterID
				break
			}
		}
	} else if len(meters) == 1 {
		meterID = meters[0].MeterID
	}

	if meterID == "" {
		return nil, fmt.Errorf("could not determine meter id: %v", funk.Map(meters, func(m discovergyMeter) string {
			return m.FullSerialNumber
		}))
	}

	uri := fmt.Sprintf("%s/last_reading?meterId=%s", discovergyAPI, meterID)
	power, err := provider.NewHTTP(log, http.MethodGet, uri, headers, "", false, "", ".values.power", 0.001*cc.Scale)
	if err != nil {
		return nil, err
	}

	return NewConfigurable(power.FloatGetter())
}

func matchesIdentifier(id string, m discovergyMeter) bool {
	return id == m.MeterID || id == m.SerialNumber || id == m.FullSerialNumber
}
