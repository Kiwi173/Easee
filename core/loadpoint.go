package core

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/core/loadpoint"
	"github.com/evcc-io/evcc/core/soc"
	"github.com/evcc-io/evcc/core/wrapper"
	"github.com/evcc-io/evcc/provider"
	"github.com/evcc-io/evcc/push"
	"github.com/evcc-io/evcc/util"

	evbus "github.com/asaskevich/EventBus"
	"github.com/avast/retry-go/v3"
	"github.com/benbjohnson/clock"
)

const (
	evChargeStart       = "start"      // update chargeTimer
	evChargeStop        = "stop"       // update chargeTimer
	evChargeCurrent     = "current"    // update fakeChargeMeter
	evChargePower       = "power"      // update chargeRater
	evVehicleConnect    = "connect"    // vehicle connected
	evVehicleDisconnect = "disconnect" // vehicle disconnected

	minActiveCurrent      = 1.0 // minimum current at which a phase is treated as active
	vehicleDetectInterval = 3 * time.Minute
	vehicleDetectDuration = 10 * time.Minute
)

// PollConfig defines the vehicle polling mode and interval
type PollConfig struct {
	Mode     string        `mapstructure:"mode"`     // polling mode charging (default), connected, always
	Interval time.Duration `mapstructure:"interval"` // interval when not charging
}

// SoCConfig defines soc settings, estimation and update behaviour
type SoCConfig struct {
	Poll     PollConfig `mapstructure:"poll"`
	Estimate bool       `mapstructure:"estimate"`
	Min      int        `mapstructure:"min"`    // Default minimum SoC, guarded by mutex
	Target   int        `mapstructure:"target"` // Default target SoC, guarded by mutex
}

// Poll modes
const (
	pollCharging  = "charging"
	pollConnected = "connected"
	pollAlways    = "always"

	pollInterval = 60 * time.Minute
)

// ThresholdConfig defines enable/disable hysteresis parameters
type ThresholdConfig struct {
	Delay     time.Duration
	Threshold float64
}

// ActionConfig defines an action to take on event
type ActionConfig struct {
	Mode      api.ChargeMode `mapstructure:"mode"`      // Charge mode to apply when car disconnected
	TargetSoC int            `mapstructure:"targetSoC"` // Target SoC to apply when car disconnected
}

// LoadPoint is responsible for controlling charge depending on
// SoC needs and power availability.
type LoadPoint struct {
	clock    clock.Clock       // mockable time
	bus      evbus.Bus         // event bus
	pushChan chan<- push.Event // notifications
	uiChan   chan<- util.Param // client push messages
	lpChan   chan<- *LoadPoint // update requests
	log      *util.Logger

	// exposed public configuration
	sync.Mutex                // guard status
	Mode       api.ChargeMode `mapstructure:"mode"` // Charge mode, guarded by mutex

	Title       string   `mapstructure:"title"`    // UI title
	Phases      int      `mapstructure:"phases"`   // Charger enabled phases
	ChargerRef  string   `mapstructure:"charger"`  // Charger reference
	VehicleRef  string   `mapstructure:"vehicle"`  // Vehicle reference
	VehiclesRef []string `mapstructure:"vehicles"` // Vehicles reference
	Meters      struct {
		ChargeMeterRef string `mapstructure:"charge"` // Charge meter reference
	}
	SoC             SoCConfig
	OnDisconnect    ActionConfig            `mapstructure:"onDisconnect"`
	OnIdentify      map[string]ActionConfig `mapstructure:"onIdentify"`
	Enable, Disable ThresholdConfig

	MinCurrent    float64       // PV mode: start current	Min+PV mode: min current
	MaxCurrent    float64       // Max allowed current. Physically ensured by the charger
	GuardDuration time.Duration // charger enable/disable minimum holding time

	enabled                bool      // Charger enabled state
	activePhases           int       // Charger active phases as used by vehicle
	chargeCurrent          float64   // Charger current limit
	guardUpdated           time.Time // Charger enabled/disabled timestamp
	socUpdated             time.Time // SoC updated timestamp (poll: connected)
	vehicleConnected       time.Time // Vehicle connected timestamp
	vehicleConnectedTicker *clock.Ticker
	vehicleID              string

	charger     api.Charger
	chargeTimer api.ChargeTimer
	chargeRater api.ChargeRater

	chargeMeter  api.Meter     // Charger usage meter
	vehicle      api.Vehicle   // Currently active vehicle
	vehicles     []api.Vehicle // Assigned vehicles
	socEstimator *soc.Estimator
	socTimer     *soc.Timer

	// cached state
	status         api.ChargeStatus       // Charger status
	remoteDemand   loadpoint.RemoteDemand // External status demand
	chargePower    float64                // Charging power
	chargeCurrents []float64              // Phase currents
	connectedTime  time.Time              // Time when vehicle was connected
	pvTimer        time.Time              // PV enabled/disable timer
	phaseTimer     time.Time              // 1p3p switch timer

	// charge progress
	vehicleSoc              float64       // Vehicle SoC
	chargeDuration          time.Duration // Charge duration
	chargedEnergy           float64       // Charged energy while connected in Wh
	chargeRemainingDuration time.Duration // Remaining charge duration
	chargeRemainingEnergy   float64       // Remaining charge energy in Wh

	tasks []func() error // task list for repeated execution
}

// NewLoadPointFromConfig creates a new loadpoint
func NewLoadPointFromConfig(log *util.Logger, cp configProvider, other map[string]interface{}) (*LoadPoint, error) {
	lp := NewLoadPoint(log)
	if err := util.DecodeOther(other, &lp); err != nil {
		return nil, err
	}

	// set vehicle polling mode
	switch lp.SoC.Poll.Mode = strings.ToLower(lp.SoC.Poll.Mode); lp.SoC.Poll.Mode {
	case pollCharging:
	case pollConnected, pollAlways:
		log.WARN.Printf("poll mode '%s' may deplete your battery or lead to API misuse. USE AT YOUR OWN RISK.", lp.SoC.Poll)
	default:
		if lp.SoC.Poll.Mode != "" {
			log.WARN.Printf("invalid poll mode: %s", lp.SoC.Poll.Mode)
		}
		lp.SoC.Poll.Mode = pollConnected
	}

	// set vehicle polling interval
	if lp.SoC.Poll.Interval < pollInterval {
		if lp.SoC.Poll.Interval == 0 {
			lp.SoC.Poll.Interval = pollInterval
		} else {
			log.WARN.Printf("poll interval '%v' is lower than %v and may deplete your battery or lead to API misuse. USE AT YOUR OWN RISK.", lp.SoC.Poll.Interval, pollInterval)
		}
	}

	if lp.SoC.Target == 0 {
		lp.SoC.Target = lp.OnDisconnect.TargetSoC // use disconnect value as default soc
		if lp.SoC.Target == 0 {
			lp.SoC.Target = 100
		}
	}

	if lp.MinCurrent == 0 {
		log.WARN.Println("minCurrent must not be zero")
	}

	if lp.MaxCurrent <= lp.MinCurrent {
		log.WARN.Println("maxCurrent must be larger than minCurrent")
	}

	if lp.Meters.ChargeMeterRef != "" {
		lp.chargeMeter = cp.Meter(lp.Meters.ChargeMeterRef)
	}

	// multiple vehicles
	for _, ref := range lp.VehiclesRef {
		vehicle := cp.Vehicle(ref)
		lp.vehicles = append(lp.vehicles, vehicle)
	}

	// single vehicle
	if lp.VehicleRef != "" {
		vehicle := cp.Vehicle(lp.VehicleRef)
		lp.vehicles = append(lp.vehicles, vehicle)
	}

	if lp.ChargerRef == "" {
		return nil, errors.New("missing charger")
	}
	lp.charger = cp.Charger(lp.ChargerRef)
	lp.configureChargerType(lp.charger)

	// allow target charge handler to access loadpoint
	lp.socTimer = soc.NewTimer(lp.log, &adapter{LoadPoint: lp})
	if lp.Enable.Threshold > lp.Disable.Threshold {
		log.WARN.Printf("PV mode enable threshold (%.0fW) is larger than disable threshold (%.0fW)", lp.Enable.Threshold, lp.Disable.Threshold)
	} else if lp.Enable.Threshold > 0 {
		log.WARN.Printf("PV mode enable threshold %.0fW > 0 will start PV charging on grid power consumption. Did you mean -%.0f?", lp.Enable.Threshold, lp.Enable.Threshold)
	}

	return lp, nil
}

// NewLoadPoint creates a LoadPoint with sane defaults
func NewLoadPoint(log *util.Logger) *LoadPoint {
	clock := clock.New()
	bus := evbus.New()

	lp := &LoadPoint{
		log:           log,   // logger
		clock:         clock, // mockable time
		bus:           bus,   // event bus
		Mode:          api.ModeOff,
		Phases:        3,
		status:        api.StatusNone,
		MinCurrent:    6,  // A
		MaxCurrent:    16, // A
		GuardDuration: 5 * time.Minute,
	}

	return lp
}

// requestUpdate requests site to update this loadpoint
func (lp *LoadPoint) requestUpdate() {
	select {
	case lp.lpChan <- lp: // request loadpoint update
	default:
	}
}

// configureChargerType ensures that chargeMeter, Rate and Timer can use charger capabilities
func (lp *LoadPoint) configureChargerType(charger api.Charger) {
	// ensure charge meter exists
	if lp.chargeMeter == nil {
		if mt, ok := charger.(api.Meter); ok {
			lp.chargeMeter = mt
		} else {
			mt := &wrapper.ChargeMeter{}
			_ = lp.bus.Subscribe(evChargeCurrent, lp.evChargeCurrentWrappedMeterHandler)
			_ = lp.bus.Subscribe(evChargeStop, func() { mt.SetPower(0) })
			lp.chargeMeter = mt
		}
	}

	// ensure charge rater exists
	if rt, ok := charger.(api.ChargeRater); ok {
		lp.chargeRater = rt
	} else {
		rt := wrapper.NewChargeRater(lp.log, lp.chargeMeter)
		_ = lp.bus.Subscribe(evChargePower, rt.SetChargePower)
		_ = lp.bus.Subscribe(evVehicleConnect, func() { rt.StartCharge(false) })
		_ = lp.bus.Subscribe(evChargeStart, func() { rt.StartCharge(true) })
		_ = lp.bus.Subscribe(evChargeStop, rt.StopCharge)
		lp.chargeRater = rt
	}

	// ensure charge timer exists
	if ct, ok := charger.(api.ChargeTimer); ok {
		lp.chargeTimer = ct
	} else {
		ct := wrapper.NewChargeTimer()
		_ = lp.bus.Subscribe(evVehicleConnect, func() { ct.StartCharge(false) })
		_ = lp.bus.Subscribe(evChargeStart, func() { ct.StartCharge(true) })
		_ = lp.bus.Subscribe(evChargeStop, ct.StopCharge)
		lp.chargeTimer = ct
	}
}

// pushEvent sends push messages to clients
func (lp *LoadPoint) pushEvent(event string) {
	lp.pushChan <- push.Event{Event: event}
}

// publish sends values to UI and databases
func (lp *LoadPoint) publish(key string, val interface{}) {
	if lp.uiChan != nil {
		lp.uiChan <- util.Param{Key: key, Val: val}
	}
}

// evChargeStartHandler sends external start event
func (lp *LoadPoint) evChargeStartHandler() {
	lp.log.INFO.Println("start charging ->")
	lp.pushEvent(evChargeStart)

	// soc update reset
	lp.socUpdated = time.Time{}
}

// evChargeStopHandler sends external stop event
func (lp *LoadPoint) evChargeStopHandler() {
	lp.log.INFO.Println("stop charging <-")
	lp.pushEvent(evChargeStop)

	// soc update reset
	lp.socUpdated = time.Time{}
}

// evVehicleConnectHandler sends external start event
func (lp *LoadPoint) evVehicleConnectHandler() {
	lp.log.INFO.Printf("car connected")

	// energy
	lp.chargedEnergy = 0
	lp.publish("chargedEnergy", lp.chargedEnergy)

	// duration
	lp.connectedTime = lp.clock.Now()
	lp.publish("connectedDuration", time.Duration(0))

	// soc update reset
	lp.socUpdated = time.Time{}

	// soc update reset on car change
	if lp.socEstimator != nil {
		lp.socEstimator.Reset()
	}

	// flush all vehicles before updating state
	lp.log.DEBUG.Println("vehicle api refresh")
	provider.ResetCached()

	// identify active vehicle
	lp.startVehicleDetection()

	// immediately allow pv mode activity
	lp.elapsePVTimer()

	lp.pushEvent(evVehicleConnect)
}

// evVehicleDisconnectHandler sends external start event
func (lp *LoadPoint) evVehicleDisconnectHandler() {
	lp.log.INFO.Println("car disconnected")

	// energy and duration
	lp.publish("chargedEnergy", lp.chargedEnergy)
	lp.publish("connectedDuration", lp.clock.Since(lp.connectedTime))

	lp.pushEvent(evVehicleDisconnect)

	// remove active vehicle
	if len(lp.vehicles) > 1 {
		lp.setActiveVehicle(nil)
	}

	// set default mode on disconnect
	lp.applyAction(lp.OnDisconnect)

	// soc update reset
	lp.socUpdated = time.Time{}
}

// evChargeCurrentHandler publishes the charge current
func (lp *LoadPoint) evChargeCurrentHandler(current float64) {
	if !lp.enabled {
		current = 0
	}
	lp.publish("chargeCurrent", current)
}

// evChargeCurrentWrappedMeterHandler updates the dummy charge meter's charge power.
// This simplifies the main flow where the charge meter can always be treated as present.
// It assumes that the charge meter cannot consume more than total household consumption.
// If physical charge meter is present this handler is not used.
// The actual value is published by the evChargeCurrentHandler
func (lp *LoadPoint) evChargeCurrentWrappedMeterHandler(current float64) {
	power := current * float64(lp.activePhases) * Voltage

	if !lp.enabled || lp.GetStatus() != api.StatusC {
		// if disabled we cannot be charging
		power = 0
	}

	// handler only called if charge meter was replaced by dummy
	lp.chargeMeter.(*wrapper.ChargeMeter).SetPower(power)
}

// applyAction executes the action
func (lp *LoadPoint) applyAction(action ActionConfig) {
	if action.Mode != "" && lp.GetMode() != api.ModeEmpty {
		lp.SetMode(action.Mode)
	}
	if action.TargetSoC != 0 {
		_ = lp.SetTargetSoC(action.TargetSoC)
	}
}

// Name returns the human-readable loadpoint title
func (lp *LoadPoint) Name() string {
	return lp.Title
}

// Prepare loadpoint configuration by adding missing helper elements
func (lp *LoadPoint) Prepare(uiChan chan<- util.Param, pushChan chan<- push.Event, lpChan chan<- *LoadPoint) {
	lp.uiChan = uiChan
	lp.pushChan = pushChan
	lp.lpChan = lpChan

	// assume all phases are active
	lp.activePhases = lp.Phases

	// event handlers
	_ = lp.bus.Subscribe(evChargeStart, lp.evChargeStartHandler)
	_ = lp.bus.Subscribe(evChargeStop, lp.evChargeStopHandler)
	_ = lp.bus.Subscribe(evVehicleConnect, lp.evVehicleConnectHandler)
	_ = lp.bus.Subscribe(evVehicleDisconnect, lp.evVehicleDisconnectHandler)
	_ = lp.bus.Subscribe(evChargeCurrent, lp.evChargeCurrentHandler)

	// publish initial values
	lp.publish("title", lp.Title)
	lp.publish("minCurrent", lp.MinCurrent)
	lp.publish("maxCurrent", lp.MaxCurrent)
	lp.publish("phases", lp.Phases)
	lp.publish("activePhases", lp.activePhases)
	lp.publish("hasVehicle", len(lp.vehicles) > 0)

	lp.Lock()
	lp.publish("mode", lp.Mode)
	lp.publish("targetSoC", lp.SoC.Target)
	lp.publish("minSoC", lp.SoC.Min)
	lp.Unlock()

	// always treat single vehicle as attached to allow poll mode: always
	if len(lp.vehicles) == 1 {
		lp.setActiveVehicle(lp.vehicles[0])
	}

	// start detection if we have multiple vehicles
	if len(lp.vehicles) > 1 {
		lp.startVehicleDetection()
	}

	// read initial charger state to prevent immediately disabling charger
	if enabled, err := lp.charger.Enabled(); err == nil {
		if lp.enabled = enabled; enabled {
			lp.guardUpdated = lp.clock.Now()
			// set defined current for use by pv mode
			_ = lp.setLimit(lp.GetMinCurrent(), false)
		}
	} else {
		lp.log.ERROR.Printf("charger: %v", err)
	}

	// allow charger to  access loadpoint
	if ctrl, ok := lp.charger.(loadpoint.Controller); ok {
		ctrl.LoadpointControl(lp)
	}
}

// syncCharger updates charger status and synchronizes it with expectations
func (lp *LoadPoint) syncCharger() {
	enabled, err := lp.charger.Enabled()
	if err == nil {
		if enabled != lp.enabled {
			lp.log.WARN.Printf("charger out of sync: expected %vd, got %vd", status[lp.enabled], status[enabled])
			err = lp.charger.Enable(lp.enabled)
		}

		if !enabled && lp.GetStatus() == api.StatusC {
			lp.log.WARN.Println("charger logic error: disabled but charging")
		}
	}

	if err != nil {
		lp.log.ERROR.Printf("charger: %v", err)
	}
}

// setLimit applies charger current limits and enables/disables accordingly
func (lp *LoadPoint) setLimit(chargeCurrent float64, force bool) (err error) {
	// set current
	if chargeCurrent != lp.chargeCurrent && chargeCurrent >= lp.GetMinCurrent() {
		if charger, ok := lp.charger.(api.ChargerEx); ok {
			err = charger.MaxCurrentMillis(chargeCurrent)
		} else {
			chargeCurrent = math.Trunc(chargeCurrent)
			err = lp.charger.MaxCurrent(int64(chargeCurrent))
		}

		if err == nil {
			lp.chargeCurrent = chargeCurrent
			lp.bus.Publish(evChargeCurrent, chargeCurrent)
			lp.log.DEBUG.Printf("max charge current: %.3gA", chargeCurrent)
		} else {
			err = fmt.Errorf("max charge current %.3g: %w", chargeCurrent, err)
		}
	}

	// set enabled/disabled
	if enabled := chargeCurrent >= lp.GetMinCurrent(); enabled != lp.enabled && err == nil {
		if remaining := (lp.GuardDuration - lp.clock.Since(lp.guardUpdated)).Truncate(time.Second); remaining > 0 && !force {
			lp.log.DEBUG.Printf("charger %s: contactor delay %v", status[enabled], remaining)
			return nil
		}

		// sleep vehicle
		if car, ok := lp.vehicle.(api.VehicleStopCharge); !enabled && ok {
			// log but don't propagate
			if err := car.StopCharge(); err != nil {
				lp.log.ERROR.Printf("vehicle remote charge stop: %v", err)
			}
		}

		lp.log.DEBUG.Printf("charger %s", status[enabled])
		if err = lp.charger.Enable(enabled); err == nil {
			lp.enabled = enabled
			lp.guardUpdated = lp.clock.Now()

			lp.bus.Publish(evChargeCurrent, chargeCurrent)

			// wake up vehicle
			if car, ok := lp.vehicle.(api.VehicleStartCharge); enabled && ok {
				// log but don't propagate
				if err := car.StartCharge(); err != nil {
					lp.log.ERROR.Printf("vehicle remote charge start: %v", err)
				}
			}
		} else {
			err = fmt.Errorf("charger %s: %w", status[enabled], err)
		}
	}

	return err
}

// connected returns the EVs connection state
func (lp *LoadPoint) connected() bool {
	status := lp.GetStatus()
	return status == api.StatusB || status == api.StatusC
}

// charging returns the EVs charging state
func (lp *LoadPoint) charging() bool {
	return lp.GetStatus() == api.StatusC
}

// charging returns the EVs charging state
func (lp *LoadPoint) setStatus(status api.ChargeStatus) {
	lp.Lock()
	defer lp.Unlock()
	lp.status = status
}

// targetSocReached checks if target is configured and reached.
// If vehicle is not configured this will always return false
func (lp *LoadPoint) targetSocReached() bool {
	return lp.vehicle != nil &&
		lp.SoC.Target > 0 &&
		lp.SoC.Target < 100 &&
		lp.vehicleSoc >= float64(lp.SoC.Target)
}

// minSocNotReached checks if minimum is configured and not reached.
// If vehicle is not configured this will always return true
func (lp *LoadPoint) minSocNotReached() bool {
	return lp.vehicle != nil &&
		lp.SoC.Min > 0 &&
		lp.vehicleSoc < float64(lp.SoC.Min)
}

// climateActive checks if vehicle has active climate request
func (lp *LoadPoint) climateActive() bool {
	if cl, ok := lp.vehicle.(api.VehicleClimater); ok {
		active, outsideTemp, targetTemp, err := cl.Climater()
		if err == nil {
			lp.log.DEBUG.Printf("climater active: %v, target temp: %.1f°C, outside temp: %.1f°C", active, targetTemp, outsideTemp)

			status := "off"
			if active {
				status = "on"

				switch {
				case outsideTemp < targetTemp:
					status = "heating"
				case outsideTemp > targetTemp:
					status = "cooling"
				}
			}

			lp.publish("climater", status)
			return active
		}

		if !errors.Is(err, api.ErrNotAvailable) {
			lp.log.ERROR.Printf("climater: %v", err)
		}
	}

	return false
}

// remoteControlled returns true if remote control status is active
func (lp *LoadPoint) remoteControlled(demand loadpoint.RemoteDemand) bool {
	lp.Lock()
	defer lp.Unlock()

	return lp.remoteDemand == demand
}

// identifyVehicle reads vehicle identification from charger
func (lp *LoadPoint) identifyVehicle() {
	identifier, ok := lp.charger.(api.Identifier)
	if !ok {
		return
	}

	id, err := identifier.Identify()
	if err != nil {
		lp.log.ERROR.Println("charger vehicle id:", err)
		return
	}

	if lp.vehicleID == id {
		return
	}

	// vehicle found or removed
	lp.vehicleID = id

	lp.log.DEBUG.Println("charger vehicle id:", id)
	lp.publish("vehicleIdentity", id)

	if id != "" {
		if vehicle := lp.selectVehicleByID(id); vehicle != nil {
			lp.setActiveVehicle(vehicle)
		}

		if action, ok := lp.OnIdentify[id]; ok {
			lp.log.DEBUG.Println("running vehicle action:", action)
			lp.applyAction(action)
		}
	}
}

// selectVehicleByID selects the vehicle with the given ID
func (lp *LoadPoint) selectVehicleByID(id string) api.Vehicle {
	// find exact match
	for _, vehicle := range lp.vehicles {
		if vid, err := vehicle.Identify(); err == nil && vid == id {
			return vehicle
		}
	}

	// find placeholder match
	for _, vehicle := range lp.vehicles {
		if vid, err := vehicle.Identify(); err == nil && vid != "" {
			re, err := regexp.Compile(strings.ReplaceAll(vid, "*", ".*?"))
			if err != nil {
				lp.log.ERROR.Printf("vehicle id: %v", err)
				continue
			}

			if re.MatchString(id) {
				return vehicle
			}
		}
	}

	return nil
}

// setActiveVehicle assigns currently active vehicle and configures soc estimator
func (lp *LoadPoint) setActiveVehicle(vehicle api.Vehicle) {
	if lp.vehicle == vehicle {
		return
	}

	from := "unknown"
	if lp.vehicle != nil {
		coordinator.release(lp.vehicle)
		from = lp.vehicle.Title()
	}
	to := "unknown"
	if vehicle != nil {
		coordinator.aquire(lp, vehicle)
		to = vehicle.Title()
	}
	lp.log.INFO.Printf("vehicle updated: %s -> %s", from, to)

	if lp.vehicle = vehicle; vehicle != nil {
		lp.socEstimator = soc.NewEstimator(lp.log, lp.charger, vehicle, lp.SoC.Estimate)

		lp.publish("vehiclePresent", true)
		lp.publish("vehicleTitle", lp.vehicle.Title())
		lp.publish("vehicleCapacity", lp.vehicle.Capacity())

		lp.task(lp.odometer)
	} else {
		lp.socEstimator = nil

		lp.publish("vehiclePresent", false)
		lp.publish("vehicleTitle", "")
		lp.publish("vehicleCapacity", int64(0))
		lp.publish("vehicleOdometer", 0.0)
	}
}

// startVehicleDetection resets connection timer and starts api refresh timer
func (lp *LoadPoint) startVehicleDetection() {
	lp.vehicleConnected = lp.clock.Now()
	lp.vehicleConnectedTicker = lp.clock.Ticker(vehicleDetectInterval)
}

// vehicleUnidentified checks if loadpoint has multiple vehicles associated and starts discovery period
func (lp *LoadPoint) vehicleUnidentified() bool {
	res := len(lp.vehicles) > 1 && lp.clock.Since(lp.vehicleConnected) < vehicleDetectDuration

	// request vehicle api refresh while waiting to identify
	if res {
		select {
		case <-lp.vehicleConnectedTicker.C:
			lp.log.DEBUG.Println("vehicle api refresh")
			provider.ResetCached()
		default:
		}
	}

	return res
}

// identifyVehicleByStatus validates if the active vehicle is still connected to the loadpoint
func (lp *LoadPoint) identifyVehicleByStatus() {
	if len(lp.vehicles) <= 1 {
		return
	}

	if vehicle := coordinator.identifyVehicleByStatus(lp.log, lp, lp.vehicles); vehicle != nil {
		lp.setActiveVehicle(vehicle)
		return
	}

	// remove previous vehicle if status was not confirmed
	if _, ok := lp.vehicle.(api.ChargeState); ok {
		lp.setActiveVehicle(nil)
	}
}

// updateChargerStatus updates charger status and detects car connected/disconnected events
func (lp *LoadPoint) updateChargerStatus() error {
	status, err := lp.charger.Status()
	if err != nil {
		return err
	}

	lp.log.DEBUG.Printf("charger status: %s", status)

	if prevStatus := lp.GetStatus(); status != prevStatus {
		lp.setStatus(status)

		// changed from empty (initial startup) - set connected without sending message
		if prevStatus == api.StatusNone {
			lp.connectedTime = lp.clock.Now()
			lp.publish("connectedDuration", time.Duration(0))
		}

		// changed from A - connected
		if prevStatus == api.StatusA {
			lp.bus.Publish(evVehicleConnect)
		}

		// changed to C - start/stop charging cycle - handle before disconnect to update energy
		if lp.charging() {
			lp.bus.Publish(evChargeStart)
		} else if prevStatus == api.StatusC {
			lp.bus.Publish(evChargeStop)
		}

		// changed to A - disconnected
		if status == api.StatusA {
			lp.bus.Publish(evVehicleDisconnect)
		}

		// update whenever there is a state change
		lp.bus.Publish(evChargeCurrent, lp.chargeCurrent)
	}

	return nil
}

// effectiveCurrent returns the currently effective charging current
func (lp *LoadPoint) effectiveCurrent() float64 {
	// adjust actual current for vehicles like Zoe where it remains below target
	if lp.chargeCurrents != nil {
		cur := lp.chargeCurrents[0]
		return math.Min(cur+2.0, lp.chargeCurrent)
	}

	if lp.GetStatus() != api.StatusC {
		return 0
	}

	return lp.chargeCurrent
}

// elapsePVTimer puts the pv enable/disable timer into elapsed state
func (lp *LoadPoint) elapsePVTimer() {
	lp.pvTimer = lp.clock.Now().Add(-lp.Disable.Delay)
	lp.guardUpdated = lp.clock.Now().Add(-lp.GuardDuration)
}

// scalePhasesIfAvailable scales if api.ChargePhases is available
func (lp *LoadPoint) scalePhasesIfAvailable(phases int) error {
	err := lp.scalePhases(phases)
	if errors.Is(err, api.ErrNotAvailable) {
		return nil
	}
	return err
}

// scalePhases adjusts the number of active phases and returns the appropriate charging current.
// Returns api.ErrNotAvailable if api.ChargePhases is not available.
func (lp *LoadPoint) scalePhases(phases int) error {
	if phases != 1 && phases != 3 {
		return fmt.Errorf("invalid number of phases: %d", phases)
	}

	cp, ok := lp.charger.(api.ChargePhases)
	if !ok {
		return api.ErrNotAvailable
	}

	lp.Lock()
	if lp.Phases != phases {
		lp.Unlock()

		// disable charger - this will also stop the car charging using the api if available
		if err := lp.setLimit(0, true); err != nil {
			return err
		}

		// switch phases
		if err := cp.Phases1p3p(phases); err != nil {
			return fmt.Errorf("switch phases: %w", err)
		}

		lp.Lock()
		lp.Phases = phases
		lp.publish("phases", lp.Phases)

		// disable phase timer
		lp.phaseTimer = time.Time{}

		// allow pv mode to re-enable charger right away
		lp.elapsePVTimer()
	}
	lp.Unlock()

	return nil
}

// pvScalePhases switches phases if necessary and returns if switch occured
func (lp *LoadPoint) pvScalePhases(availablePower, minCurrent, maxCurrent float64) bool {
	var waiting bool

	phases := lp.GetPhases()
	targetCurrent := availablePower / Voltage / float64(lp.activePhases)

	if phases < lp.activePhases {
		lp.log.WARN.Printf("invalid status: %dp active @ %dp configured", lp.activePhases, phases)
	}

	lp.log.DEBUG.Printf("!!pvScalePhases available power %.0f for target current %.1f @ %dp/%dp", availablePower, targetCurrent, lp.activePhases, phases)
	if lp.phaseTimer.IsZero() {
		lp.log.DEBUG.Printf("!!pvScalePhases timer empty")
	} else {
		lp.log.DEBUG.Printf("!!pvScalePhases timer remaining: %v", lp.clock.Since(lp.phaseTimer).Truncate(time.Second))
	}

	// scale down phases
	if targetCurrent < minCurrent && phases > 1 && lp.activePhases > 1 {
		lp.log.DEBUG.Printf("available power below %dp min threshold of %.0fW", lp.activePhases, float64(lp.activePhases)*Voltage*minCurrent)

		if lp.phaseTimer.IsZero() {
			lp.log.DEBUG.Printf("start phase disable timer: %v", lp.Disable.Delay)
			lp.phaseTimer = lp.clock.Now()
		}

		elapsed := lp.clock.Since(lp.phaseTimer)
		if elapsed >= lp.Disable.Delay {
			lp.log.DEBUG.Println("phase disable timer elapsed")
			if err := lp.scalePhases(1); err == nil {
				lp.log.DEBUG.Printf("switched phases: 1p @ %.0fW", availablePower)

				// if charging is disabled, current detection will not switch active phases to 1p
				// make sure we can start charging by assuming 1p during next cycle
				lp.activePhases = 1

				return true
			} else {
				lp.log.ERROR.Printf("switch phases: %v", err)
			}
		}

		waiting = true
		lp.log.DEBUG.Printf("phase disable timer remaining: %v", (lp.Disable.Delay - elapsed).Round(time.Second))
	}

	// scale up phases
	if min3pCurrent := powerToCurrent(availablePower, 3); min3pCurrent >= minCurrent && phases == 1 {
		lp.log.DEBUG.Printf("available power above 3p min threshold of %.0fW", 3*Voltage*minCurrent)

		if lp.phaseTimer.IsZero() {
			lp.log.DEBUG.Printf("start phase enable timer: %v", lp.Enable.Delay)
			lp.phaseTimer = lp.clock.Now()
		}

		elapsed := lp.clock.Since(lp.phaseTimer)
		if elapsed >= lp.Disable.Delay {
			lp.log.DEBUG.Println("phase enable timer elapsed")
			if err := lp.scalePhases(3); err == nil {
				lp.log.DEBUG.Printf("switched phases: 3p @ %.0fW", availablePower)
				return true
			} else {
				lp.log.ERROR.Printf("switch phases: %v", err)
			}
		}

		waiting = true
		lp.log.DEBUG.Printf("phase enable timer remaining: %v", (lp.Disable.Delay - elapsed).Round(time.Second))
	}

	// reset timer to disabled state
	if !waiting && !lp.phaseTimer.IsZero() {
		lp.log.DEBUG.Printf("phase timer reset")
		lp.phaseTimer = time.Time{}
	}

	return false
}

// pvMaxCurrent calculates the maximum target current for PV mode
func (lp *LoadPoint) pvMaxCurrent(mode api.ChargeMode, sitePower float64) float64 {
	// read only once to simplify testing
	minCurrent := lp.GetMinCurrent()
	maxCurrent := lp.GetMaxCurrent()

	// calculate target charge current from delta power and actual current
	effectiveCurrent := lp.effectiveCurrent()
	deltaCurrent := powerToCurrent(-sitePower, lp.activePhases)
	targetCurrent := math.Max(effectiveCurrent+deltaCurrent, 0)

	lp.log.DEBUG.Printf("max charge current: %.3gA = %.3gA + %.3gA (%.0fW @ %dp)", targetCurrent, effectiveCurrent, deltaCurrent, sitePower, lp.activePhases)

	// switch phases up/down
	if _, ok := lp.charger.(api.ChargePhases); ok {
		availablePower := -sitePower + lp.chargePower

		// in case of scaling, keep charger disabled for this cycle
		if lp.pvScalePhases(availablePower, minCurrent, maxCurrent) {
			return 0
		}
	}

	// in MinPV mode return at least minCurrent
	if mode == api.ModeMinPV && targetCurrent < minCurrent {
		return minCurrent
	}

	if mode == api.ModePV && lp.enabled && targetCurrent < minCurrent {
		// kick off disable sequence
		if sitePower >= lp.Disable.Threshold {
			lp.log.DEBUG.Printf("site power %.0fW >= disable threshold %.0fW", sitePower, lp.Disable.Threshold)

			if lp.pvTimer.IsZero() {
				lp.log.DEBUG.Printf("start pv disable timer: %v", lp.Disable.Delay)
				lp.pvTimer = lp.clock.Now()
			}

			elapsed := lp.clock.Since(lp.pvTimer)
			if elapsed >= lp.Disable.Delay {
				lp.log.DEBUG.Println("pv disable timer elapsed")
				return 0
			}

			lp.log.DEBUG.Printf("pv disable timer remaining: %v", (lp.Disable.Delay - elapsed).Round(time.Second))
		} else {
			// reset timer
			lp.log.DEBUG.Printf("reset pv disable timer: %v", lp.Disable.Delay)
			lp.pvTimer = lp.clock.Now()
		}

		lp.log.DEBUG.Println("pv enable timer: keep enabled")
		return minCurrent
	}

	if mode == api.ModePV && !lp.enabled {
		// kick off enable sequence
		if (lp.Enable.Threshold == 0 && targetCurrent >= minCurrent) ||
			(lp.Enable.Threshold != 0 && sitePower <= lp.Enable.Threshold) {
			lp.log.DEBUG.Printf("site power %.0fW < enable threshold %.0fW", sitePower, lp.Enable.Threshold)

			if lp.pvTimer.IsZero() {
				lp.log.DEBUG.Printf("start pv enable timer: %v", lp.Enable.Delay)
				lp.pvTimer = lp.clock.Now()
			}

			elapsed := lp.clock.Since(lp.pvTimer)
			if elapsed >= lp.Enable.Delay {
				lp.log.DEBUG.Println("pv enable timer elapsed")
				return minCurrent
			}

			lp.log.DEBUG.Printf("pv enable timer remaining: %v", (lp.Enable.Delay - elapsed).Round(time.Second))
		} else {
			// reset timer
			lp.log.DEBUG.Printf("reset pv enable timer: %v", lp.Enable.Delay)
			lp.pvTimer = lp.clock.Now()
		}

		lp.log.DEBUG.Println("pv enable timer: keep disabled")
		return 0
	}

	// reset timer to disabled state
	if !lp.pvTimer.IsZero() {
		lp.log.DEBUG.Printf("pv timer reset")
		lp.pvTimer = time.Time{}
	}

	// cap at maximum current
	targetCurrent = math.Min(targetCurrent, maxCurrent)

	return targetCurrent
}

// updateChargePower updates charge meter power
func (lp *LoadPoint) updateChargePower() {
	err := retry.Do(func() error {
		value, err := lp.chargeMeter.CurrentPower()
		if err != nil {
			return err
		}

		lp.chargePower = value // update value if no error
		lp.log.DEBUG.Printf("charge power: %.0fW", value)
		lp.publish("chargePower", value)

		return nil
	}, retryOptions...)

	if err != nil {
		lp.log.ERROR.Printf("charge meter: %v", err)
	}
}

// updateChargeCurrents uses MeterCurrent interface to count phases with current >=1A
func (lp *LoadPoint) updateChargeCurrents() {
	lp.chargeCurrents = nil
	phaseMeter, ok := lp.chargeMeter.(api.MeterCurrent)
	if !ok {
		// guess active phases from power consumption
		// assumes that chargePower has been updated before
		if lp.charging() && lp.chargeCurrent > 0 {
			phases := int(math.Round(lp.chargePower / Voltage / lp.chargeCurrent))
			if phases >= 1 && phases <= 3 {
				lp.activePhases = phases
				lp.log.DEBUG.Printf("detected phases: %dp (%.1fA @ %.0fW)", lp.activePhases, lp.chargeCurrent, lp.chargePower)
				lp.publish("activePhases", lp.activePhases)
			}
		}

		return
	}

	i1, i2, i3, err := phaseMeter.Currents()
	if err != nil {
		lp.log.ERROR.Printf("charge meter: %v", err)
		return
	}

	lp.chargeCurrents = []float64{i1, i2, i3}
	lp.log.DEBUG.Printf("charge currents: %.3gA", lp.chargeCurrents)
	lp.publish("chargeCurrents", lp.chargeCurrents)

	if lp.charging() {
		var phases int
		for _, i := range lp.chargeCurrents {
			if i >= minActiveCurrent {
				phases++
			}
		}

		if phases >= 1 {
			lp.activePhases = phases
			lp.log.DEBUG.Printf("detected phases: %dp %.3gA", lp.activePhases, lp.chargeCurrents)
			lp.publish("activePhases", lp.activePhases)
		}
	}
}

// publish charged energy and duration
func (lp *LoadPoint) publishChargeProgress() {
	if f, err := lp.chargeRater.ChargedEnergy(); err == nil {
		lp.chargedEnergy = 1e3 * f // convert to Wh
	} else {
		lp.log.ERROR.Printf("charge rater: %v", err)
	}

	if d, err := lp.chargeTimer.ChargingTime(); err == nil {
		lp.chargeDuration = d.Round(time.Second)
	} else {
		lp.log.ERROR.Printf("charge timer: %v", err)
	}

	lp.publish("chargedEnergy", lp.chargedEnergy)
	lp.publish("chargeDuration", lp.chargeDuration)
}

// socPollAllowed validates charging state against polling mode
func (lp *LoadPoint) socPollAllowed() bool {
	remaining := lp.SoC.Poll.Interval - lp.clock.Since(lp.socUpdated)

	honourUpdateInterval := lp.SoC.Poll.Mode == pollAlways ||
		lp.SoC.Poll.Mode == pollConnected && lp.connected()

	if honourUpdateInterval && remaining > 0 {
		lp.log.DEBUG.Printf("next soc poll remaining time: %v", remaining.Truncate(time.Second))
	}

	return lp.charging() || honourUpdateInterval && (remaining <= 0) || lp.connected() && lp.socUpdated.IsZero()
}

// checks if the connected charger can provide SoC to the connected vehicle
func (lp *LoadPoint) socProvidedByCharger() bool {
	if charger, ok := lp.charger.(api.Battery); ok {
		if _, err := charger.SoC(); err == nil {
			return true
		}
	}
	return false
}

// publish state of charge, remaining charge duration and range
func (lp *LoadPoint) publishSoCAndRange() {
	if lp.socEstimator == nil {
		return
	}

	if lp.socPollAllowed() || lp.socProvidedByCharger() {
		lp.socUpdated = lp.clock.Now()

		f, err := lp.socEstimator.SoC(lp.chargedEnergy)
		if err == nil {
			lp.vehicleSoc = math.Trunc(f)
			lp.log.DEBUG.Printf("vehicle soc: %.0f%%", lp.vehicleSoc)
			lp.publish("vehicleSoC", lp.vehicleSoc)

			if lp.charging() {
				lp.setRemainingDuration(lp.socEstimator.RemainingChargeDuration(lp.chargePower, lp.SoC.Target))
			} else {
				lp.setRemainingDuration(-1)
			}

			lp.setRemainingEnergy(1e3 * lp.socEstimator.RemainingChargeEnergy(lp.SoC.Target))
		} else {
			if errors.Is(err, api.ErrMustRetry) {
				lp.socUpdated = time.Time{}
			} else {
				lp.log.ERROR.Printf("vehicle soc: %v", err)
			}
		}

		// range
		if vs, ok := lp.vehicle.(api.VehicleRange); ok {
			if rng, err := vs.Range(); err == nil {
				lp.log.DEBUG.Printf("vehicle range: %vkm", rng)
				lp.publish("range", rng)
			}
		}

		return
	}

	// reset if poll: connected/charging and not connected
	if lp.SoC.Poll.Mode != pollAlways && !lp.connected() {
		lp.publish("vehicleSoC", -1)
		lp.publish("chargeRemainingDuration", time.Duration(-1))

		// range
		lp.publish("range", -1)
	}
}

// Update is the main control function. It reevaluates meters and charger state
func (lp *LoadPoint) Update(sitePower float64, cheap bool) {
	mode := lp.GetMode()
	lp.publish("mode", mode)

	// read and publish meters first
	lp.updateChargePower()
	lp.updateChargeCurrents()

	// update ChargeRater here to make sure initial meter update is caught
	lp.bus.Publish(evChargeCurrent, lp.chargeCurrent)
	lp.bus.Publish(evChargePower, lp.chargePower)

	// update progress and soc before status is updated
	lp.publishChargeProgress()

	// read and publish status
	if err := lp.updateChargerStatus(); err != nil {
		lp.log.ERROR.Printf("charger: %v", err)
		return
	}

	lp.publish("connected", lp.connected())
	lp.publish("charging", lp.charging())
	lp.publish("enabled", lp.enabled)

	// identify connected vehicle
	if lp.connected() {
		// read identity and run associated action
		lp.identifyVehicle()

		// find vehicle by status for a couple of minutes after connecting
		if lp.vehicleUnidentified() {
			lp.identifyVehicleByStatus()
		}
	}

	// odometer etc, if active
	lp.runTasks()

	// publish soc after updating charger status to make sure
	// initial update of connected state matches charger status
	lp.publishSoCAndRange()

	// sync settings with charger
	lp.syncCharger()

	// check if car connected and ready for charging
	var err error

	// track if remote disabled is actually active
	remoteDisabled := loadpoint.RemoteEnable

	// execute loading strategy
	switch {
	case !lp.connected():
		// always disable charger if not connected
		// https://github.com/evcc-io/evcc/issues/105
		err = lp.setLimit(0, false)

	case lp.targetSocReached():
		lp.log.DEBUG.Printf("targetSoC reached: %.1f > %d", lp.vehicleSoc, lp.SoC.Target)
		var targetCurrent float64 // zero disables
		if lp.climateActive() {
			lp.log.DEBUG.Println("climater active")
			targetCurrent = lp.GetMinCurrent()
		}
		err = lp.setLimit(targetCurrent, true)
		lp.socTimer.Reset() // once SoC is reached, the target charge request is removed

	// OCPP has priority over target charging
	case lp.remoteControlled(loadpoint.RemoteHardDisable):
		remoteDisabled = loadpoint.RemoteHardDisable
		fallthrough

	case mode == api.ModeOff:
		err = lp.setLimit(0, true)

	case lp.minSocNotReached():
		// 3p if available
		if err = lp.scalePhasesIfAvailable(3); err == nil {
			err = lp.setLimit(lp.GetMaxCurrent(), true)
		}
		lp.elapsePVTimer() // let PV mode disable immediately afterwards

	case mode == api.ModeNow:
		// 3p if available
		if err = lp.scalePhasesIfAvailable(3); err == nil {
			err = lp.setLimit(lp.GetMaxCurrent(), true)
		}

	// target charging
	case lp.socTimer.DemandActive() && false:
		targetCurrent := lp.socTimer.Handle()
		err = lp.setLimit(targetCurrent, true)

	case mode == api.ModeMinPV || mode == api.ModePV:
		targetCurrent := lp.pvMaxCurrent(mode, sitePower)
		lp.log.DEBUG.Printf("pv max charge current: %.3gA", targetCurrent)

		var required bool // false
		if targetCurrent == 0 && lp.climateActive() {
			targetCurrent = lp.GetMaxCurrent()
			required = true
		}

		// tariff
		if cheap {
			targetCurrent = lp.GetMaxCurrent()
			lp.log.DEBUG.Printf("cheap tariff: %.3gA", targetCurrent)
			required = true
		}

		// Sunny Home Manager
		if lp.remoteControlled(loadpoint.RemoteSoftDisable) {
			remoteDisabled = loadpoint.RemoteSoftDisable
			targetCurrent = 0
			required = true
		}

		err = lp.setLimit(targetCurrent, required)
	}

	// effective disabled status
	if remoteDisabled != loadpoint.RemoteEnable {
		lp.publish("remoteDisabled", remoteDisabled)
	}

	if err != nil {
		lp.log.ERROR.Println(err)
	}
}
