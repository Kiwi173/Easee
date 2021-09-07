package mqtt

import (
	"errors"
	"fmt"

	"github.com/evcc-io/evcc/api"
)

type clientRegistry map[string]*Client

func (r clientRegistry) Add(broker string, client *Client) {
	if _, exists := r[broker]; exists {
		panic(fmt.Sprintf("cannot register duplicate broker: %s", broker))
	}
	r[broker] = client
}

func (r clientRegistry) Get(broker string) (*Client, error) {
	client, exists := r[broker]
	if !exists {
		return nil, fmt.Errorf("missing mqtt broker configuration: %s", broker)
	}
	return client, nil
}

// registry is the Mqtt client registry
var registry clientRegistry = make(map[string]*Client)

// RegisteredClient reuses an registered Mqtt publisher or creates a new one
func RegisteredClient(log api.Logger, broker, user, password, clientID string, qos byte, opts ...Option) (*Client, error) {
	key := fmt.Sprintf("%s.%s", broker, user)
	client, err := registry.Get(key)

	if err != nil {
		if client, err = NewClient(log, broker, user, password, clientID, qos, opts...); err == nil {
			registry.Add(key, client)
		}
	}

	return client, err
}

// RegisteredClientOrDefault reuses an registered Mqtt publisher or creates a new one.
// If no publisher is configured, it uses the default instance.
func RegisteredClientOrDefault(log api.Logger, cc Config) (*Client, error) {
	var err error
	client := Instance

	if cc.Broker != "" {
		client, err = RegisteredClient(log, cc.Broker, cc.User, cc.Password, ClientID(), 1)
	}

	if client == nil && err == nil {
		err = errors.New("missing mqtt broker configuration")
	}

	return client, err
}
