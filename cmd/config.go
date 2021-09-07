package cmd

import (
	"fmt"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/charger"
	"github.com/evcc-io/evcc/meter"
	"github.com/evcc-io/evcc/provider/mqtt"
	"github.com/evcc-io/evcc/push"
	"github.com/evcc-io/evcc/server"
	"github.com/evcc-io/evcc/vehicle"
)

type config struct {
	URI          string
	Log          string
	SponsorToken string
	Metrics      bool
	Profile      bool
	Levels       map[string]string
	Interval     time.Duration
	Mqtt         mqttConfig
	Javascript   map[string]interface{}
	Influx       server.InfluxConfig
	EEBus        map[string]interface{}
	HEMS         typedConfig
	Messaging    messagingConfig
	Meters       []qualifiedConfig
	Chargers     []qualifiedConfig
	Vehicles     []qualifiedConfig
	Tariffs      tariffConfig
	Site         map[string]interface{}
	LoadPoints   []map[string]interface{}
}

type mqttConfig struct {
	mqtt.Config `mapstructure:",squash"`
	Topic       string
}

func (conf *mqttConfig) RootTopic() string {
	if conf.Topic != "" {
		return conf.Topic
	}
	return "evcc"
}

type qualifiedConfig struct {
	Name, Type string
	Other      map[string]interface{} `mapstructure:",remain"`
}

type typedConfig struct {
	Type  string
	Other map[string]interface{} `mapstructure:",remain"`
}

type messagingConfig struct {
	Events   map[string]push.EventTemplate
	Services []typedConfig
}

type tariffConfig struct {
	Grid typedConfig
}

// ConfigProvider provides configuration items
type ConfigProvider struct {
	meters   map[string]api.Meter
	chargers map[string]api.Charger
	vehicles map[string]api.Vehicle
}

// Meter provides meters by name
func (cp *ConfigProvider) Meter(name string) api.Meter {
	if meter, ok := cp.meters[name]; ok {
		return meter
	}
	log.Fatalf("invalid meter: %s", name)
	return nil
}

// Charger provides chargers by name
func (cp *ConfigProvider) Charger(name string) api.Charger {
	if charger, ok := cp.chargers[name]; ok {
		return charger
	}
	log.Fatalf("invalid charger: %s", name)
	return nil
}

// Vehicle provides vehicles by name
func (cp *ConfigProvider) Vehicle(name string) api.Vehicle {
	if vehicle, ok := cp.vehicles[name]; ok {
		return vehicle
	}
	log.Fatalf("invalid vehicle: %s", name)
	return nil
}

func (cp *ConfigProvider) configure(conf config) error {
	err := cp.configureMeters(conf)
	if err == nil {
		err = cp.configureChargers(conf)
	}
	if err == nil {
		err = cp.configureVehicles(conf)
	}
	return err
}

func (cp *ConfigProvider) configureMeters(conf config) error {
	cp.meters = make(map[string]api.Meter)
	for id, cc := range conf.Meters {
		if cc.Name == "" {
			return fmt.Errorf("cannot create %s meter: missing name", humanize.Ordinal(id+1))
		}

		m, err := meter.NewFromConfig(cc.Type, cc.Other)
		if err != nil {
			err = fmt.Errorf("cannot create meter '%s': %w", cc.Name, err)
			return err
		}

		if _, exists := cp.meters[cc.Name]; exists {
			return fmt.Errorf("duplicate meter name: %s already defined and must be unique", cc.Name)
		}

		cp.meters[cc.Name] = m
	}

	return nil
}

func (cp *ConfigProvider) configureChargers(conf config) error {
	cp.chargers = make(map[string]api.Charger)
	for id, cc := range conf.Chargers {
		if cc.Name == "" {
			return fmt.Errorf("cannot create %s charger: missing name", humanize.Ordinal(id+1))
		}

		c, err := charger.NewFromConfig(cc.Type, cc.Other)
		if err != nil {
			err = fmt.Errorf("cannot create charger '%s': %w", cc.Name, err)
			return err
		}

		if _, exists := cp.chargers[cc.Name]; exists {
			return fmt.Errorf("duplicate charger name: %s already defined and must be unique", cc.Name)
		}

		cp.chargers[cc.Name] = c
	}

	return nil
}

func (cp *ConfigProvider) configureVehicles(conf config) error {
	cp.vehicles = make(map[string]api.Vehicle)
	for id, cc := range conf.Vehicles {
		if cc.Name == "" {
			return fmt.Errorf("cannot create %s vehicle: missing name", humanize.Ordinal(id+1))
		}

		v, err := vehicle.NewFromConfig(cc.Type, cc.Other)
		if err != nil {
			return fmt.Errorf("cannot create vehicle '%s': %w", cc.Name, err)
		}

		if _, exists := cp.vehicles[cc.Name]; exists {
			return fmt.Errorf("duplicate vehicle name: %s already defined and must be unique", cc.Name)
		}

		cp.vehicles[cc.Name] = v
	}

	return nil
}
