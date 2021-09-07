package vehicle

import (
	"fmt"
	"strings"
	"time"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/util"
	"github.com/evcc-io/evcc/vehicle/nissan"
)

// Credits to
//   https://github.com/Tobiaswk/dartnissanconnect
//   https://github.com/mitchellrj/kamereon-python
//   https://gitlab.com/tobiaswkjeldsen/carwingsflutter

// OAuth base url
// 	 https://prod.eu.auth.kamereon.org/kauth/oauth2/a-ncb-prod/.well-known/openid-configuration

// Nissan is an api.Vehicle implementation for Nissan cars
type Nissan struct {
	*embed
	*nissan.Provider
}

func init() {
	registry.Add("nissan", NewNissanFromConfig)
}

// NewNissanFromConfig creates a new vehicle
func NewNissanFromConfig(other map[string]interface{}) (api.Vehicle, error) {
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

	v := &Nissan{
		embed: &cc.embed,
	}

	log := util.NewLogger("nissan")
	identity := nissan.NewIdentity(log)

	if err := identity.Login(cc.User, cc.Password); err != nil {
		return v, fmt.Errorf("login failed: %w", err)
	}

	api := nissan.NewAPI(log, identity, strings.ToUpper(cc.VIN))

	var err error
	if cc.VIN == "" {
		api.VIN, err = findVehicle(api.Vehicles())
		if err == nil {
			log.Debugf("found vehicle: %v", api.VIN)
		}
	}

	v.Provider = nissan.NewProvider(api, cc.Cache)

	return v, err
}
