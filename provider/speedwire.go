package provider

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/andig/evcc-config/registry"
	"github.com/andig/evcc/api"
	"github.com/andig/evcc/util"
	"github.com/imdario/mergo"
	"gitlab.com/bboehmke/sunny"
)

const udpTimeout = 10 * time.Second

// smaDiscoverer discovers SMA devices in background while providing already found devices
type smaDiscoverer struct {
	conn    *sunny.Connection
	devices map[uint32]*sunny.Device
	mux     sync.RWMutex
	done    uint32
}

// run discover and store found devices
func (d *smaDiscoverer) run() {
	devices := make(chan *sunny.Device)

	go func() {
		for device := range devices {
			d.mux.Lock()
			d.devices[device.SerialNumber()] = device
			d.mux.Unlock()
		}
	}()

	// discover devices and wait for results
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	d.conn.DiscoverDevices(ctx, devices, "")
	cancel()
	close(devices)

	// mark discover as done
	atomic.AddUint32(&d.done, 1)
}

func (d *smaDiscoverer) get(serial uint32) *sunny.Device {
	d.mux.RLock()
	defer d.mux.RUnlock()
	return d.devices[serial]
}

// deviceBySerial with the given serial number
func (d *smaDiscoverer) deviceBySerial(serial uint32) *sunny.Device {
	start := time.Now()
	for time.Since(start) < time.Second*3 {
		// discover done -> return immediately regardless of result
		if atomic.LoadUint32(&d.done) != 0 {
			return d.get(serial)
		}

		// device with serial found -> return
		if device := d.get(serial); device != nil {
			return device
		}

		time.Sleep(time.Millisecond * 10)
	}
	return d.get(serial)
}

// Speedwire supporting SMA Home Manager 2.0 and SMA Energy Meter 30
type Speedwire struct {
	log    *util.Logger
	mux    *util.Waiter
	uri    string
	iface  string
	values map[string]interface{}
	scale  float64
	device *sunny.Device
}

func init() {
	registry.Add("speedwire", NewSpeedwireFromConfig)
}

// NewSpeedwireFromConfig creates SMA Speedwire provider
func NewSpeedwireFromConfig(other map[string]interface{}) (IntProvider, error) {
	cc := struct {
		URI, Password, Interface string
		Serial                   uint32
		Value                    string
		Scale                    float64 // power only
	}{
		Password: "0000",
		Scale:    1,
	}

	if err := util.DecodeOther(other, &cc); err != nil {
		return nil, err
	}

	return NewSpeedwire(cc.URI, cc.Password, cc.Interface, cc.Serial, cc.Scale)
}

// map of created discover instances
var discoverers = make(map[string]*smaDiscoverer)

// NewSpeedwire creates a SMA Meter
func NewSpeedwire(uri, password, iface string, serial uint32, scale float64) (api.Meter, error) {
	log := util.NewLogger("speedwire")
	sunny.Log = log.TRACE

	sm := &SMA{
		mux:    util.NewWaiter(udpTimeout, func() { log.TRACE.Println("wait for initial value") }),
		log:    log,
		uri:    uri,
		iface:  iface,
		values: make(map[string]interface{}),
		scale:  scale,
	}

	conn, err := sunny.NewConnection(iface)
	if err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}

	switch {
	case uri != "":
		sm.device, err = conn.NewDevice(uri, password)
		if err != nil {
			return nil, err
		}

	case serial > 0:
		discoverer, ok := discoverers[iface]
		if !ok {
			discoverer = &smaDiscoverer{
				conn:    conn,
				devices: make(map[uint32]*sunny.Device),
			}

			go discoverer.run()

			discoverers[iface] = discoverer
		}

		sm.device = discoverer.deviceBySerial(serial)
		if sm.device == nil {
			return nil, fmt.Errorf("device not found: %d", serial)
		}
		sm.device.SetPassword(password)

	default:
		return nil, errors.New("missing uri or serial")
	}

	go func() {
		for range time.NewTicker(time.Second).C {
			sm.updateValues()
		}
	}()

	return sm, nil
}

func (sm *Speedwire) updateValues() {
	sm.mux.Lock()
	defer sm.mux.Unlock()

	values, err := sm.device.GetValues()
	if err == nil {
		err = mergo.Merge(&sm.values, values)
	}

	if err == nil {
		sm.mux.Update()
	} else {
		sm.log.ERROR.Println(err)
	}
}

func (sm *Speedwire) hasValue() (map[string]interface{}, error) {
	elapsed := sm.mux.LockWithTimeout()
	defer sm.mux.Unlock()

	if elapsed > 0 {
		return nil, fmt.Errorf("update timeout: %v", elapsed.Truncate(time.Second))
	}

	return sm.values, nil
}

// CurrentPower implements the api.Meter interface
func (sm *Speedwire) CurrentPower() (float64, error) {
	values, err := sm.hasValue()

	var power float64
	if sm.device.IsEnergyMeter() {
		power = sm.asFloat(values["active_power_plus"]) - sm.asFloat(values["active_power_minus"])
	} else {
		power = sm.asFloat(values["power_ac_total"])
	}

	return sm.scale * power, err
}

func (sm *Speedwire) asFloat(value interface{}) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case uint32:
		return float64(v)
	case uint64:
		return float64(v)
	case nil:
		return 0
	default:
		sm.log.WARN.Printf("unknown value type: %T", value)
		return 0
	}
}
