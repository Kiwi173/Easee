package skoda

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/util/oauth"
	"github.com/evcc-io/evcc/util/request"
	"golang.org/x/oauth2"
)

type tokenRefresher struct {
	*request.Helper
	login func() (oauth.Token, error)
}

func Refresher(log api.Logger, login func() (oauth.Token, error)) oauth.TokenRefresher {
	return &tokenRefresher{
		Helper: request.NewHelper(log),
		login:  login,
	}
}

// RefreshToken implements oauth.TokenRefresher
func (tr *tokenRefresher) RefreshToken(token *oauth2.Token) (*oauth2.Token, error) {
	uri := "https://tokenrefreshservice.apps.emea.vwapps.io/refreshTokens"

	data := url.Values(map[string][]string{
		"grant_type":    {"refresh_token"},
		"refresh_token": {token.RefreshToken},
		"brand":         {"skoda"},
	})

	req, err := request.New(http.MethodPost, uri, strings.NewReader(data.Encode()), request.URLEncoding)

	var res oauth.Token
	if err == nil {
		err = tr.DoJSON(req, &res)
	}

	if se, ok := err.(request.StatusError); ok && se.HasStatus(http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden) {
		res, err = tr.login()
	}

	return (*oauth2.Token)(&res), err
}
