package vehicle

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/andig/evcc/api"
	"github.com/andig/evcc/util"
	"github.com/andig/evcc/util/request"
	"github.com/andig/evcc/vehicle/vw"
	"github.com/shurcooL/graphql"
	"golang.org/x/oauth2"
)

// https://github.com/davidgiga1993/AudiAPI
// https://github.com/TA2k/ioBroker.vw-connect

// Audi is an api.Vehicle implementation for Audi cars
type Audi struct {
	*embed
	*vw.Provider // provides the api implementations
}

func init() {
	registry.Add("audi", NewAudiFromConfig)
}

// NewAudiFromConfig creates a new vehicle
func NewAudiFromConfig(other map[string]interface{}) (api.Vehicle, error) {
	cc := struct {
		embed               `mapstructure:",squash"`
		User, Password, VIN string
		Cache               time.Duration
		Timeout             time.Duration
	}{
		Cache:   interval,
		Timeout: request.Timeout,
	}

	if err := util.DecodeOther(other, &cc); err != nil {
		return nil, err
	}

	v := &Audi{
		embed: &cc.embed,
	}

	log := util.NewLogger("audi")
	identity := vw.NewIdentity(log)

	query := url.Values(map[string][]string{
		"response_type": {"id_token token"},
		"client_id":     {"09b6cbec-cd19-4589-82fd-363dfa8c24da@apps_vw-dilab_com"},
		"redirect_uri":  {"myaudi:///"},
		"scope":         {"openid profile mbb vin badge birthdate nickname email address phone name picture"},
		"prompt":        {"login"},
		"ui_locales":    {"de-DE"},
	})

	err := identity.LoginVAG("77869e21-e30a-4a92-b016-48ab7d3db1d8", query, cc.User, cc.Password)
	if err != nil {
		return v, fmt.Errorf("login failed: %w", err)
	}

	api := vw.NewAPI(log, identity, "Audi", "DE")
	api.Client.Timeout = cc.Timeout

	if cc.VIN == "" {
		cc.VIN, err = findVehicle(api.Vehicles())
		if err == nil {
			log.DEBUG.Printf("found vehicle: %v", cc.VIN)
		}
	}

	if err == nil {
		if err = api.HomeRegion(strings.ToUpper(cc.VIN)); err == nil {
			v.Provider = vw.NewProvider(api, strings.ToUpper(cc.VIN), cc.Cache)
		}
	}

	_, _ = v.Images2(identity, cc.VIN)

	return v, err
}

type AudiVehicles struct {
	Vehicles []AudiVehicle
}

type AudiVehicle struct {
	VIN       string
	ShortName string
	ImageUrl  string
}

func (v *Audi) Images(identity *vw.Identity, vin string) ([]string, error) {
	helper := request.NewHelper(util.NewLogger("IMAGE"))
	helper.Client.Transport = &oauth2.Transport{
		Source: oauth2.StaticTokenSource(&oauth2.Token{
			AccessToken: identity.AccessToken(),
		}),
		Base: helper.Client.Transport,
	}

	uri := "https://api.my.audi.com/smns/v1/navigation/v1/vehicles"
	// uri := fmt.Sprintf("https://api.my.audi.com/smns/v1/navigation/v1/vehicles/%s", vin)

	req, err := request.New(http.MethodGet, uri, nil, map[string]string{
		"X-Market": "de_DE",
	})

	var res AudiVehicles
	if err == nil {
		err = helper.DoJSON(req, &res)
		fmt.Println(res)
	}

	panic(err)
}

// curl https://livem2.retailservices.audi.de/patp/v1/vgql/v1/graphql \
// -H "Content-Type: application/json" \
// -H "Accept: application/json" \
// -H "Accept-Language: de-DE" \
// -H "Authorization: Bearer eyJraWQiOiJiMDdhZDQ2ODFmYzc3OTc1IiwiYWxnIjoiUlMyNTYifQ.eyJzdWIiOiI0MTg3ODJjMS1lZGM5LTQ0ZWQtODg5ZC05MjA3ODJjMDA3NjQiLCJhdWQiOiJjN2MxNWU3Zi0xMzVjLTRiZDMtOTg3NS02MzgzODYxNjUwOWZAYXBwc192dy1kaWxhYl9jb20iLCJzY3AiOiJwcm9maWxlIGJhZGdlIGJpcnRoZGF0ZSBhZGRyZXNzIHBob25lIHZpbiBuYXRpb25hbGl0eSBwcm9mZXNzaW9uIG5hdGlvbmFsSWRlbnRpZmllciBiaXJ0aHBsYWNlIG9wZW5pZCIsImFhdCI6ImlkZW50aXR5a2l0IiwiaXNzIjoiaHR0cHM6XC9cL2lkZW50aXR5LnZ3Z3JvdXAuaW8iLCJqdHQiOiJhY2Nlc3NfdG9rZW4iLCJleHAiOjE2MjcyMTgxMzUsImlhdCI6MTYyNzIxNDUzNSwibGVlIjpbIkFVREkiXSwianRpIjoiYTQ2NGQyMzEtNzE3ZS00ZTg3LWExMDMtNzIxMGYyYTIyMjhhIn0.vUo80Rg53sMVVonmxm7uB8dpRiYMbfVBosLIiS_BNq90HipMAb54QHsWXdbZVcb6QVbSMt-qClgjrBzNmLpm_VZLR0HEDxhVzEF6gllEh72qjCs5deiqiMJC7NojT3FhTggclij7XNcVdNWNcrO98odGVrotSSCRBvpEf5lrYIyN2BZOz3eJm8o42imeQ81oG1IH1th5tqlVf1GmuaPYOn0u01Amtqs9PXurhV5Yh7K0DRmZbCXzuqAHylcqUTTpg4slBFJLj1QU8qoTOSwoFDSEC7OCjFemvrwWfeim4j-dfEDyDMFz3Ks3eq2v_q-jRWPlTOGDUFkjm6uVEwM21Kv0A5QWnZomR-HlzJg7Gh5-uA-E0eszZW5UjA0oeLEJSNQhZ5ZcEE8I1d7_GHnf-z22UmyCw8Br943HLEkFzvnCT6QdXcx412hmqtZKwjt_91A5kvilNl6pGWoxbo288-yAqhL13RYSXDf1RuStt4j4oh-lJNEhCxjmjz8tDLol4l0q1N2zVoJZcXbbziOWh7EKkq9HzL8N4ZTuHJ9VQt23KbATEn4W88MZxjFlyvF6Tt3NqoBEXxJePJizIoX0_nojA8V1vxQMJdXY3ZJ_L0od6gt4WXN4WCviuhjHFLk7nUtjbfqn1ugKKLyDDSi2aoAkFYaig-dt7d0VOn8-_L8" \
// -d '{"query":" query ($vin: String!) {userVehicle(vehicleCoreId: $vin) {vin owner type devicePlatform mbbConnect userRole {role} vehicle {media {shortName} renderPictures {mediaType url}}}} ","variables":{"vin":"WAUZZZFY1L2018852"}}' | jq

func (v *Audi) Images2(identity *vw.Identity, vin string) ([]string, error) {
	helper := request.NewHelper(util.NewLogger("IMAGE"))
	helper.Client.Transport = &oauth2.Transport{
		Source: oauth2.StaticTokenSource(&oauth2.Token{
			AccessToken: identity.AccessToken(),
		}),
		Base: helper.Client.Transport,
	}

	uri := "https://livem2.retailservices.audi.de/patp/v1/vgql/v1/graphql"

	var res struct {
		UserVehicle struct {
			VIN     string
			Vehicle struct {
				Media struct {
					ShortName string
				}
				RenderPictures []struct {
					MediaType, URL string
				}
			}
		} `graphql:"userVehicle(vehicleCoreId: $vin)"`
	}

	ctx := context.WithValue(
		context.Background(),
		oauth2.HTTPClient,
		request.NewHelper(util.NewLogger("IMAGE")).Client,
	)

	client := oauth2.NewClient(ctx, oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: identity.IDToken(),
	}))

	gqlClient := graphql.NewClient(uri, client)

	gqlClient.RequestFactory = func(method, url string, body io.Reader) (*http.Request, error) {
		req, err := http.NewRequest(method, url, body)
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept-Language", "de-DE")
		}
		return req, err
	}

	err := gqlClient.Query(context.Background(), &res, map[string]interface{}{
		"vin": graphql.String(vin),
	})

	fmt.Println(res)
	panic(err)

	return nil, err
}
