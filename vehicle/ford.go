package vehicle

import (
	"errors"
	"time"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/util"
	"github.com/evcc-io/evcc/vehicle/ford"
)

// Ford is an api.Vehicle implementation for Ford cars
type Ford struct {
	*embed
	*ford.Provider
}

func init() {
	registry.Add("ford", NewFordFromConfig)
}

// NewFordFromConfig creates a new vehicle
func NewFordFromConfig(other map[string]interface{}) (api.Vehicle, error) {
	cc := struct {
		embed               `mapstructure:",squash"`
		User, Password, VIN string
		Expiry              time.Duration
		Cache               time.Duration
	}{
		// Expiry: expiry,
		Cache: interval,
	}

	if err := util.DecodeOther(other, &cc); err != nil {
		return nil, err
	}

	if cc.User == "" || cc.Password == "" {
		return nil, errors.New("missing credentials")
	}

	v := &Ford{
		embed: &cc.embed,
	}

	log := util.NewLogger("ford")
	identity := ford.NewIdentity(log)

	if err := identity.Login(cc.User, cc.Password); err != nil {
		return nil, err
	}

	// token, err := v.login()
	// if err == nil {
	// 	v.tokenSource = oauth.RefreshTokenSource((*oauth2.Token)(&token), v)
	// }

	api := ford.NewAPI(log, identity)

	// v.statusG = provider.NewCached(func() (interface{}, error) {
	// 	return v.status()
	// }, cc.Cache).InterfaceGetter()

	var err error
	if cc.VIN == "" {
		cc.VIN, err = findVehicle(api.Vehicles())
		if err == nil {
			log.DEBUG.Printf("found vehicle: %v", cc.VIN)
		}
	}

	v.Provider = ford.NewProvider(api, cc.VIN, cc.Cache)

	return v, err
}
