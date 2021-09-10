package fiat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentity"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentity/types"
	"github.com/evcc-io/evcc/util"
	"github.com/evcc-io/evcc/util/request"
)

const (
	LoginURI = "https://loginmyuconnect.fiat.com"
	TokenURI = "https://authz.sdpr-01.fcagcv.com/v2/cognito/identity/token"

	Region = "eu-west-1"
)

type Identity struct {
	*request.Helper
	user, password string
	uid            string
	creds          *types.Credentials
}

// NewIdentity creates Fiat identity
func NewIdentity(log *util.Logger, user, password string) *Identity {
	return &Identity{
		Helper:   request.NewHelper(log),
		user:     user,
		password: password,
	}
}

// Login authenticates with username/password to get new aws credentials
func (v *Identity) Login() error {
	v.Client.Jar, _ = cookiejar.New(nil)

	uri := fmt.Sprintf("%s/accounts.webSdkBootstrap", LoginURI)

	data := url.Values(map[string][]string{
		"APIKey":   {ApiKey},
		"pageURL":  {"https://myuconnect.fiat.com/de/de/vehicle-services"},
		"sdk":      {"js_latest"},
		"sdkBuild": {"12234"},
		"format":   {"json"},
	})

	headers := map[string]string{
		"Accept": "*/*",
	}

	req, err := request.New(http.MethodGet, uri, nil, headers)
	if err == nil {
		req.URL.RawQuery = data.Encode()

		var resp *http.Response
		if resp, err = v.Do(req); err == nil {
			resp.Body.Close()
		}
	}

	var res struct {
		ErrorCode    int
		UID          string
		StatusReason string
		SessionInfo  struct {
			LoginToken string `json:"login_token"`
			ExpiresIn  string `json:"expires_in"`
		}
	}

	if err == nil {
		uri = fmt.Sprintf("%s/accounts.login", LoginURI)

		data := url.Values(map[string][]string{
			"loginID":           {v.user},
			"password":          {v.password},
			"sessionExpiration": {"7776000"},
			"APIKey":            {ApiKey},
			"pageURL":           {"https://myuconnect.fiat.com/de/de/login"},
			"sdk":               {"js_latest"},
			"sdkBuild":          {"12234"},
			"format":            {"json"},
			"targetEnv":         {"jssdk"},
			"include":           {"profile,data,emails"}, // subscriptions,preferences
			"includeUserInfo":   {"true"},
			"loginMode":         {"standard"},
			"lang":              {"de0de"},
			"source":            {"showScreenSet"},
			"authMode":          {"cookie"},
		})

		headers := map[string]string{
			"Accept":       "*/*",
			"Content-Type": "application/x-www-form-urlencoded",
		}

		if req, err = request.New(http.MethodPost, uri, strings.NewReader(data.Encode()), headers); err == nil {
			if err = v.DoJSON(req, &res); err == nil {
				v.uid = res.UID
			}
		}
	}

	var token struct {
		ErrorCode    int `json:"errorCode"`
		StatusReason string
		IDToken      string `json:"id_token"`
	}

	if err == nil {
		uri = fmt.Sprintf("%s/accounts.getJWT", LoginURI)

		data := url.Values(map[string][]string{
			"fields":      {"profile.firstName,profile.lastName,profile.email,country,locale,data.disclaimerCodeGSDP"}, // data.GSDPisVerified
			"APIKey":      {ApiKey},
			"pageURL":     {"https://myuconnect.fiat.com/de/de/dashboard"},
			"sdk":         {"js_latest"},
			"sdkBuild":    {"12234"},
			"format":      {"json"},
			"login_token": {res.SessionInfo.LoginToken},
			"authMode":    {"cookie"},
		})

		headers := map[string]string{
			"Accept": "*/*",
		}

		if req, err = request.New(http.MethodGet, uri, nil, headers); err == nil {
			req.URL.RawQuery = data.Encode()
			err = v.DoJSON(req, &token)
		}
	}

	var identity struct {
		Token, IdentityID string
	}

	if err == nil {
		data := struct {
			GigyaToken string `json:"gigya_token"`
		}{
			GigyaToken: token.IDToken,
		}

		headers := map[string]string{
			"Content-Type":        "application/json",
			"X-Clientapp-Version": "1.0",
			"ClientRequestId":     util.RandomString(16),
			"X-Api-Key":           XApiKey,
			"X-Originator-Type":   "web",
		}

		if req, err = request.New(http.MethodPost, TokenURI, request.MarshalJSON(data), headers); err == nil {
			err = v.DoJSON(req, &identity)
		}
	}

	var cfg aws.Config
	if err == nil {
		cfg, err = config.LoadDefaultConfig(context.TODO(), config.WithRegion(Region))
	}

	var credRes *cognitoidentity.GetCredentialsForIdentityOutput
	if err == nil {
		svc := cognitoidentity.NewFromConfig(cfg)

		credRes, err = svc.GetCredentialsForIdentity(context.TODO(),
			&cognitoidentity.GetCredentialsForIdentityInput{
				IdentityId: &identity.IdentityID,
				Logins: map[string]string{
					"cognito-identity.amazonaws.com": identity.Token,
				},
			})
	}

	if err == nil {
		v.creds = credRes.Credentials
	}

	return err
}

// UID returns the logged in users uid
func (v *Identity) UID() string {
	return v.uid
}

// Sign signs an AWS request using identity's credentials
func (v *Identity) Sign(req *http.Request, body io.ReadSeeker) error {
	// refresh credentials
	if v.creds.Expiration.After(time.Now().Add(-time.Minute)) {
		if err := v.Login(); err != nil {
			return fmt.Errorf("login failed: %w", err)
		}
	}

	// sign request
	ctx := context.TODO()
	provider := credentials.NewStaticCredentialsProvider(*v.creds.AccessKeyId, *v.creds.SecretKey, *v.creds.SessionToken)
	creds, err := provider.Retrieve(ctx)

	var hashBytes []byte
	if err == nil {
		if body == nil {
			body = strings.NewReader("")
		}
		hashBytes, err = sha256FromReader(body)
	}

	if err == nil {
		sha256Hash := hex.EncodeToString(hashBytes)
		signer := v4.NewSigner()
		err = signer.SignHTTP(ctx, creds, req, sha256Hash, "execute-api", Region, time.Now())
	}

	return err
}

// https://github.com/bernays/appsyncgo
func sha256FromReader(reader io.ReadSeeker) (hashBytes []byte, err error) {
	hash := sha256.New()
	start, err := reader.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}

	defer func() {
		// ensure error is returned if unable to seek back to start if payload
		_, err = reader.Seek(start, io.SeekStart)
	}()

	_, err = io.Copy(hash, reader)
	return hash.Sum(nil), err
}
