package meter

import (
	"errors"
	"fmt"
	"time"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/util"
	"github.com/evcc-io/evcc/util/modbus"
	"github.com/volkszaehler/mbmd/meters"
	"github.com/volkszaehler/mbmd/meters/rs485"
	"github.com/volkszaehler/mbmd/meters/sunspec"
)

// Modbus is an api.Meter implementation with configurable getters and setters.
type Modbus struct {
	log      api.Logger
	conn     *modbus.Connection
	device   meters.Device
	opPower  modbus.Operation
	opEnergy modbus.Operation
	opSoC    modbus.Operation
}

func init() {
	registry.Add("modbus", NewModbusFromConfig)
}

//go:generate go run ../cmd/tools/decorate.go -f decorateModbus -b api.Meter -t "api.MeterEnergy,TotalEnergy,func() (float64, error)" -t "api.Battery,SoC,func() (float64, error)"

// NewModbusFromConfig creates api.Meter from config
func NewModbusFromConfig(other map[string]interface{}) (api.Meter, error) {
	cc := struct {
		Model              string
		modbus.Settings    `mapstructure:",squash"`
		Power, Energy, SoC string
		Timeout            time.Duration
	}{
		Power: "Power",
		Settings: modbus.Settings{
			ID: 1,
		},
	}

	if err := util.DecodeOther(other, &cc); err != nil {
		return nil, err
	}

	// assume RTU if not set and this is a known RS485 meter model
	if cc.RTU == nil {
		b := modbus.IsRS485(cc.Model)
		cc.RTU = &b
	}

	format := modbus.TcpFormat
	if cc.RTU != nil && *cc.RTU {
		format = modbus.RtuFormat
	}

	conn, err := modbus.NewConnection(cc.URI, cc.Device, cc.Comset, cc.Baudrate, format, cc.ID)
	if err != nil {
		return nil, err
	}

	// set non-default timeout
	if cc.Timeout > 0 {
		conn.Timeout(cc.Timeout)
	}

	log := util.NewLogger("modbus")
	conn.Logger(log.TRACE)

	// prepare device
	device, err := modbus.NewDevice(cc.Model, cc.SubDevice)

	if err == nil {
		err = device.Initialize(conn)

		// silence Kostal implementation errors
		if errors.Is(err, meters.ErrPartiallyOpened) {
			err = nil
		}
	}

	if err != nil {
		return nil, err
	}

	m := &Modbus{
		log:    log,
		conn:   conn,
		device: device,
	}

	cc.Power = modbus.ReadingName(cc.Power)
	if err := modbus.ParseOperation(device, cc.Power, &m.opPower); err != nil {
		return nil, fmt.Errorf("invalid measurement for power: %s", cc.Power)
	}

	// decorate energy reading
	var totalEnergy func() (float64, error)
	if cc.Energy != "" {
		cc.Energy = modbus.ReadingName(cc.Energy)
		if err := modbus.ParseOperation(device, cc.Energy, &m.opEnergy); err != nil {
			return nil, fmt.Errorf("invalid measurement for energy: %s", cc.Energy)
		}

		totalEnergy = m.totalEnergy
	}

	// decorate soc reading
	var soc func() (float64, error)
	if cc.SoC != "" {
		cc.SoC = modbus.ReadingName(cc.SoC)
		if err := modbus.ParseOperation(device, cc.SoC, &m.opSoC); err != nil {
			return nil, fmt.Errorf("invalid measurement for soc: %s", cc.SoC)
		}

		soc = m.soc
	}

	return decorateModbus(m, totalEnergy, soc), nil
}

// floatGetter executes configured modbus read operation and implements func() (float64, error)
func (m *Modbus) floatGetter(op modbus.Operation) (float64, error) {
	var res meters.MeasurementResult
	var err error

	if dev, ok := m.device.(*rs485.RS485); ok {
		res, err = dev.QueryOp(m.conn, op.MBMD)
	}

	if dev, ok := m.device.(*sunspec.SunSpec); ok {
		if op.MBMD.IEC61850 != 0 {
			res, err = dev.QueryOp(m.conn, op.MBMD.IEC61850)
		} else {
			res, err = dev.QueryPoint(
				m.conn,
				op.SunSpec.Model,
				op.SunSpec.Block,
				op.SunSpec.Point,
			)
		}
	}

	// silence NaN reading errors by assuming zero
	if err != nil && errors.Is(err, meters.ErrNaN) {
		res.Value = 0
		err = nil
	}

	if err == nil {
		m.log.Tracef("%+v", res)
	}

	return res.Value, err
}

// CurrentPower implements the api.Meter interface
func (m *Modbus) CurrentPower() (float64, error) {
	return m.floatGetter(m.opPower)
}

// totalEnergy implements the api.MeterEnergy interface
func (m *Modbus) totalEnergy() (float64, error) {
	return m.floatGetter(m.opEnergy)
}

// soc implements the api.Battery interface
func (m *Modbus) soc() (float64, error) {
	return m.floatGetter(m.opSoC)
}
