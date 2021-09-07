package charger

// LICENSE

// Copyright (c) 2019-2021 andig

// This module is NOT covered by the MIT license. All rights reserved.

// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.

// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

import (
	"fmt"
	"net/http"
	"time"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/charger/easee"
	"github.com/evcc-io/evcc/core/loadpoint"
	"github.com/evcc-io/evcc/util"
	"github.com/evcc-io/evcc/util/request"
	"github.com/evcc-io/evcc/util/sponsor"
	"github.com/thoas/go-funk"
	"golang.org/x/oauth2"
)

// Easee charger implementation
type Easee struct {
	*request.Helper
	charger       string
	site, circuit int
	status        easee.ChargerStatus
	updated       time.Time
	cache         time.Duration
	lp            loadpoint.API
	//lastSmartCharging bool
	//lastChargeMode api.ChargeMode
	log     *util.Logger
	phases  int
	current float64
}

func init() {
	registry.Add("easee", NewEaseeFromConfig)
}

// NewEaseeFromConfig creates a go-e charger from generic config
func NewEaseeFromConfig(other map[string]interface{}) (api.Charger, error) {
	cc := struct {
		User     string
		Password string
		Charger  string
		Circuit  int
		Cache    time.Duration
	}{
		Cache: 10 * time.Second,
	}

	if err := util.DecodeOther(other, &cc); err != nil {
		return nil, err
	}

	return NewEasee(cc.User, cc.Password, cc.Charger, cc.Circuit, cc.Cache)
}

// NewEasee creates Easee charger
func NewEasee(user, password, charger string, circuit int, cache time.Duration) (*Easee, error) {
	log := util.NewLogger("easee")

	if !sponsor.IsAuthorized() {
		return nil, api.ErrSponsorRequired
	}

	c := &Easee{
		Helper:  request.NewHelper(log),
		charger: charger,
		circuit: circuit,
		cache:   cache,
		log:     log,
		phases:  3,
	}

	ts, err := easee.TokenSource(log, user, password)
	if err != nil {
		return c, err
	}

	// replace client transport with authenticated transport
	c.Client.Transport = &oauth2.Transport{
		Source: ts,
		Base:   c.Client.Transport,
	}

	// find site
	site, err := c.chargerDetails()
	if err != nil {
		return c, err
	}

	c.site = site.ID

	// find charger
	if charger == "" {
		chargers, err := c.chargers()
		if err != nil {
			return c, err
		}

		if len(chargers) != 1 {
			return c, fmt.Errorf("cannot determine charger id, found: %v", funk.Map(chargers, func(c easee.Charger) string { return c.ID }))
		}

		c.charger = chargers[0].ID
	}

	// find circuit
	if circuit == 0 {
		if len(site.Circuits) != 1 {
			return c, fmt.Errorf("cannot determine circuit id, found: %v", funk.Map(site.Circuits, func(c easee.Circuit) int { return c.ID }))
		}

		c.circuit = site.Circuits[0].ID
	}

	return c, err
}

func (c *Easee) chargers() (res []easee.Charger, err error) {
	uri := fmt.Sprintf("%s/chargers", easee.API)

	req, err := request.New(http.MethodGet, uri, nil, request.JSONEncoding)
	if err == nil {
		err = c.DoJSON(req, &res)
	}

	return res, err
}

func (c *Easee) chargerDetails() (res easee.Site, err error) {
	uri := fmt.Sprintf("%s/chargers/%s/site", easee.API, c.charger)

	req, err := request.New(http.MethodGet, uri, nil, request.JSONEncoding)
	if err == nil {
		err = c.DoJSON(req, &res)
	}

	return res, err
}

/*
func (c *Easee) syncSmartCharging() error {
	if c.lp == nil {
		return nil
	}

	if c.lp.GetMode() != c.lastChargeMode {
		c.log.Debugf("charge mode changed by loadpoint: %v -> %v", c.lastChargeMode, c.lp.GetMode())
		newSmartCharging := false
		if c.lp.GetMode() == api.ModePV {
			newSmartCharging = true
		}

		data := easee.ChargerSettings{
			SmartCharging: &newSmartCharging,
		}

		uri := fmt.Sprintf("%s/chargers/%s/settings", easee.API, c.charger)
		resp, err := c.Post(uri, request.JSONContent, request.MarshalJSON(data))
		if err == nil {
			resp.Body.Close()
		}

		c.updated = time.Time{} // clear cache

		c.lastChargeMode = c.lp.GetMode()
		c.lastSmartCharging = newSmartCharging

		return err
	}

	if c.lastSmartCharging != c.status.SmartCharging {
		c.log.Debugf("smart status changed by charger: %v -> %v", c.lastSmartCharging, c.status.SmartCharging)
		if c.status.SmartCharging {
			c.lp.SetMode(api.ModePV)
		} else {
			c.lp.SetMode(api.ModeNow)
		}
		c.lastSmartCharging = c.status.SmartCharging
		c.lastChargeMode = c.lp.GetMode()
	}
	return nil
}
*/

func (c *Easee) state() (easee.ChargerStatus, error) {
	if time.Since(c.updated) < c.cache {
		return c.status, nil
	}

	uri := fmt.Sprintf("%s/chargers/%s/state", easee.API, c.charger)
	req, err := request.New(http.MethodGet, uri, nil, request.JSONEncoding)
	if err == nil {
		if err = c.DoJSON(req, &c.status); err == nil {
			// err = c.syncSmartCharging()
			c.updated = time.Now()
		}
	}

	return c.status, err
}

// Status implements the api.Charger interface
func (c *Easee) Status() (api.ChargeStatus, error) {
	res, err := c.state()
	if err != nil {
		return api.StatusNone, err
	}

	switch res.ChargerOpMode {
	case easee.ModeDisconnected:
		return api.StatusA, nil
	case easee.ModeAwaitingStart, easee.ModeCompleted, easee.ModeReadyToCharge:
		return api.StatusB, nil
	case easee.ModeCharging:
		return api.StatusC, nil
	case easee.ModeError:
		return api.StatusF, nil
	default:
		return api.StatusNone, fmt.Errorf("unknown opmode: %d", res.ChargerOpMode)
	}
}

// Enabled implements the api.Charger interface
func (c *Easee) Enabled() (bool, error) {
	res, err := c.state()
	return res.ChargerOpMode == easee.ModeCharging || res.ChargerOpMode == easee.ModeReadyToCharge, err
}

// Enable implements the api.Charger interface
func (c *Easee) Enable(enable bool) error {
	res, err := c.state()
	if err != nil {
		return err
	}

	// enable charger once
	if enable && !res.IsOnline {
		data := easee.ChargerSettings{
			Enabled: &enable,
		}

		uri := fmt.Sprintf("%s/chargers/%s/settings", easee.API, c.charger)
		resp, err := c.Post(uri, request.JSONContent, request.MarshalJSON(data))
		if err == nil {
			resp.Body.Close()
		}

		c.updated = time.Time{} // clear cache

		return err
	}

	// resume/stop charger
	action := "pause_charging"
	if enable {
		action = "resume_charging"
	}

	uri := fmt.Sprintf("%s/chargers/%s/commands/%s", easee.API, c.charger, action)
	_, err = c.Post(uri, request.JSONContent, nil)
	c.updated = time.Time{} // clear cache

	return err
}

// MaxCurrent implements the api.Charger interface
func (c *Easee) MaxCurrent(current int64) error {
	return c.MaxCurrentMillis(float64(current))
}

var _ api.ChargerEx = (*Easee)(nil)

// MaxCurrentMillis implements the api.ChargerEx interface
func (c *Easee) MaxCurrentMillis(current float64) error {
	var current23 float64
	if c.phases > 1 {
		current23 = current
	}

	data := easee.CircuitSettings{
		DynamicCircuitCurrentP1: &current,
		DynamicCircuitCurrentP2: &current23,
		DynamicCircuitCurrentP3: &current23,
	}

	uri := fmt.Sprintf("%s/sites/%d/circuits/%d/settings", easee.API, c.site, c.circuit)
	resp, err := c.Post(uri, request.JSONContent, request.MarshalJSON(data))
	if err == nil {
		resp.Body.Close()

		c.updated = time.Time{} // clear cache
		c.current = current
	}

	return err
}

var _ api.ChargePhases = (*Easee)(nil)

// Phases1p3p implements the api.ChargePhases interface
func (c *Easee) Phases1p3p(phases int) error {
	c.phases = phases
	return c.MaxCurrentMillis(c.current)
}

var _ api.Meter = (*Easee)(nil)

// CurrentPower implements the api.Meter interface
func (c *Easee) CurrentPower() (float64, error) {
	res, err := c.state()
	return 1e3 * res.TotalPower, err
}

var _ api.ChargeRater = (*Easee)(nil)

// ChargedEnergy implements the api.ChargeRater interface
func (c *Easee) ChargedEnergy() (float64, error) {
	res, err := c.state()
	return res.SessionEnergy, err
}

var _ api.MeterCurrent = (*Easee)(nil)

// Currents implements the api.MeterCurrent interface
func (c *Easee) Currents() (float64, float64, float64, error) {
	res, err := c.state()
	return res.CircuitTotalPhaseConductorCurrentL1,
		res.CircuitTotalPhaseConductorCurrentL2,
		res.CircuitTotalPhaseConductorCurrentL3,
		err
}

var _ loadpoint.Controller = (*Easee)(nil)

// LoadpointControl implements loadpoint.Controller
func (c *Easee) LoadpointControl(lp loadpoint.API) {
	c.lp = lp
}
