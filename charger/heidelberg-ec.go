package charger

// LICENSE

// Copyright (c) 2019-2021 andig

// This module is NOT covered by the MIT license. All rights reserved.

// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.

// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

import (
	"encoding/binary"
	"fmt"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/util"
	"github.com/evcc-io/evcc/util/modbus"
	"github.com/evcc-io/evcc/util/sponsor"
)

// HeidelbergEC charger implementation
type HeidelbergEC struct {
	log     api.Logger
	conn    *modbus.Connection
	current uint16
}

const (
	hecRegFirmware      = 1   // Input
	hecRegVehicleStatus = 5   // Input
	hecRegTemperature   = 9   // Input
	hecRegPower         = 14  // Input
	hecRegEnergy        = 17  // Input
	hecRegStandby       = 258 // Holding
	hecRegAmpsConfig    = 261 // Holding
)

var hecRegCurrents = []uint16{6, 7, 8}

func init() {
	registry.Add("heidelberg", NewHeidelbergECFromConfig)
}

// https://wallbox.heidelberg.com/wp-content/uploads/2021/05/EC_ModBus_register_table_20210222.pdf (newer)
// https://cdn.shopify.com/s/files/1/0101/2409/9669/files/heidelberg-energy-control-modbus.pdf (older)

// NewHeidelbergECFromConfig creates a HeidelbergEC charger from generic config
func NewHeidelbergECFromConfig(other map[string]interface{}) (api.Charger, error) {
	cc := modbus.Settings{
		ID: 1,
	}

	if err := util.DecodeOther(other, &cc); err != nil {
		return nil, err
	}

	return NewHeidelbergEC(cc.URI, cc.Device, cc.Comset, cc.Baudrate, cc.ID)
}

// NewHeidelbergEC creates HeidelbergEC charger
func NewHeidelbergEC(uri, device, comset string, baudrate int, slaveID uint8) (api.Charger, error) {
	conn, err := modbus.NewConnection(uri, device, comset, baudrate, modbus.RtuFormat, slaveID)
	if err != nil {
		return nil, err
	}

	if !sponsor.IsAuthorized() {
		return nil, api.ErrSponsorRequired
	}

	log := util.NewLogger("hec")
	conn.Logger(log.TRACE)

	wb := &HeidelbergEC{
		log:     log,
		conn:    conn,
		current: 60, // assume min current
	}

	return wb, nil
}

// Status implements the api.Charger interface
func (wb *HeidelbergEC) Status() (api.ChargeStatus, error) {
	b, err := wb.conn.ReadInputRegisters(hecRegVehicleStatus, 1)
	if err != nil {
		return api.StatusNone, err
	}

	switch sb := b[1]; sb {
	case 2, 3:
		return api.StatusA, nil
	case 4, 5:
		return api.StatusB, nil
	case 6, 7:
		return api.StatusC, nil
	default:
		return api.StatusNone, fmt.Errorf("invalid status: %0x", sb)
	}
}

// Enabled implements the api.Charger interface
func (wb *HeidelbergEC) Enabled() (bool, error) {
	b, err := wb.conn.ReadHoldingRegisters(hecRegAmpsConfig, 1)
	if err != nil {
		return false, err
	}

	cur := binary.BigEndian.Uint16(b)

	enabled := cur != 0
	if enabled {
		wb.current = cur
	}

	return enabled, nil
}

// Enable implements the api.Charger interface
func (wb *HeidelbergEC) Enable(enable bool) error {
	var cur uint16
	if enable {
		cur = wb.current
	}

	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, cur)

	_, err := wb.conn.WriteMultipleRegisters(hecRegAmpsConfig, 1, b)

	return err
}

// MaxCurrent implements the api.Charger interface
func (wb *HeidelbergEC) MaxCurrent(current int64) error {
	if current < 6 {
		return fmt.Errorf("invalid current %d", current)
	}

	return wb.MaxCurrentMillis(float64(current))
}

var _ api.ChargerEx = (*HeidelbergEC)(nil)

// MaxCurrentMillis implements the api.ChargerEx interface
func (wb *HeidelbergEC) MaxCurrentMillis(current float64) error {
	if current < 6 {
		return fmt.Errorf("invalid current %.1f", current)
	}

	cur := uint16(10 * current)

	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, cur)

	_, err := wb.conn.WriteMultipleRegisters(hecRegAmpsConfig, 1, b)
	if err == nil {
		wb.current = cur
	}

	return err
}

var _ api.Meter = (*HeidelbergEC)(nil)

// CurrentPower implements the api.Meter interface
func (wb *HeidelbergEC) CurrentPower() (float64, error) {
	b, err := wb.conn.ReadInputRegisters(hecRegPower, 1)
	if err != nil {
		return 0, err
	}

	return float64(binary.BigEndian.Uint16(b)), nil
}

var _ api.MeterEnergy = (*HeidelbergEC)(nil)

// TotalEnergy implements the api.MeterEnergy interface
func (wb *HeidelbergEC) TotalEnergy() (float64, error) {
	b, err := wb.conn.ReadInputRegisters(hecRegEnergy, 2)
	if err != nil {
		return 0, err
	}

	return float64(binary.BigEndian.Uint32(b)) / 1e3, nil
}

var _ api.MeterCurrent = (*HeidelbergEC)(nil)

// Currents implements the api.MeterCurrent interface
func (wb *HeidelbergEC) Currents() (float64, float64, float64, error) {
	var currents []float64
	for _, regCurrent := range hecRegCurrents {
		b, err := wb.conn.ReadInputRegisters(regCurrent, 1)
		if err != nil {
			return 0, 0, 0, err
		}

		currents = append(currents, float64(binary.BigEndian.Uint16(b))/10)
	}

	return currents[0], currents[1], currents[2], nil
}

var _ api.Diagnosis = (*HeidelbergEC)(nil)

// Diagnose implements the api.Diagnosis interface
func (wb *HeidelbergEC) Diagnose() {
	if b, err := wb.conn.ReadInputRegisters(hecRegFirmware, 2); err == nil {
		fmt.Printf("Firmware:\t%d.%d.%d\n", b[1], b[2], b[3])
	}
	if b, err := wb.conn.ReadInputRegisters(hecRegTemperature, 1); err == nil {
		fmt.Printf("Temperature:\t%.1fC\n", float64(int16(binary.BigEndian.Uint16(b)))/10)
	}
	if b, err := wb.conn.ReadHoldingRegisters(hecRegStandby, 1); err == nil {
		fmt.Printf("Standby:\t%0x\n", b[1])
	}
}
