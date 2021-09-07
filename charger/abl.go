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

// ABLeMH charger implementation
type ABLeMH struct {
	log  api.Logger
	conn *modbus.Connection
}

const (
	ablRegFirmware   = 0x01
	ablRegStatus     = 0x04
	ablRegMode       = 0x05
	ablRegAmpsConfig = 0x14
	ablRegStatusLong = 0x2E

	ablStatusDisabled = 0xE0

	ablAmpsDisabled uint16 = 0x03E8
	ablModeEnable   uint16 = 0xA1A1
	ablModeDisable  uint16 = 0xE0E0
)

func init() {
	registry.Add("abl", NewABLeMHFromConfig)
}

// https://www.goingelectric.de/forum/viewtopic.php?p=1550459#p1550459

// NewABLeMHFromConfig creates a ABLeMH charger from generic config
func NewABLeMHFromConfig(other map[string]interface{}) (api.Charger, error) {
	cc := modbus.Settings{
		ID: 1,
	}

	if err := util.DecodeOther(other, &cc); err != nil {
		return nil, err
	}

	return NewABLeMH(cc.URI, cc.Device, cc.Comset, cc.Baudrate, cc.ID)
}

// NewABLeMH creates ABLeMH charger
func NewABLeMH(uri, device, comset string, baudrate int, slaveID uint8) (api.Charger, error) {
	conn, err := modbus.NewConnection(uri, device, comset, baudrate, modbus.AsciiFormat, slaveID)
	if err != nil {
		return nil, err
	}

	if !sponsor.IsAuthorized() {
		return nil, api.ErrSponsorRequired
	}

	log := util.NewLogger("abl")
	conn.Logger(log.TRACE)

	wb := &ABLeMH{
		log:  log,
		conn: conn,
	}

	return wb, nil
}

// Status implements the api.Charger interface
func (wb *ABLeMH) Status() (api.ChargeStatus, error) {
	_, _ = wb.conn.ReadHoldingRegisters(ablRegStatus, 1)
	b, err := wb.conn.ReadHoldingRegisters(ablRegStatus, 1)
	if err != nil {
		return api.StatusNone, err
	}

	r := rune(b[1]>>4-0x0A) + 'A'

	switch r {
	case 'A', 'B', 'C':
		return api.ChargeStatus(r), nil
	default:
		return api.StatusNone, fmt.Errorf("invalid status: %v", r)
	}
}

// Enabled implements the api.Charger interface
func (wb *ABLeMH) Enabled() (bool, error) {
	_, _ = wb.conn.ReadHoldingRegisters(ablRegStatus, 1)
	b, err := wb.conn.ReadHoldingRegisters(ablRegStatus, 1)
	if err != nil {
		return false, err
	}

	enabled := b[1] != ablStatusDisabled

	return enabled, nil
}

// Enable implements the api.Charger interface
func (wb *ABLeMH) Enable(enable bool) error {
	u := ablModeDisable
	if enable {
		u = ablModeEnable
	}

	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, u)

	_, _ = wb.conn.WriteMultipleRegisters(ablRegMode, 1, b)
	_, err := wb.conn.WriteMultipleRegisters(ablRegMode, 1, b)

	return err
}

// MaxCurrent implements the api.Charger interface
func (wb *ABLeMH) MaxCurrent(current int64) error {
	return wb.MaxCurrentMillis(float64(current))
}

var _ api.ChargerEx = (*ABLeMH)(nil)

// MaxCurrent implements the api.ChargerEx interface
func (wb *ABLeMH) MaxCurrentMillis(current float64) error {
	// calculate duty cycle according to https://www.goingelectric.de/forum/viewtopic.php?p=1575287#p1575287
	u := uint16(current / 0.06)

	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, u)

	_, _ = wb.conn.WriteMultipleRegisters(ablRegAmpsConfig, 1, b)
	_, err := wb.conn.WriteMultipleRegisters(ablRegAmpsConfig, 1, b)

	return err
}

var _ api.Meter = (*ABLeMH)(nil)

// CurrentPower implements the api.Meter interface
func (wb *ABLeMH) CurrentPower() (float64, error) {
	l1, l2, l3, err := wb.Currents()
	return 230 * (l1 + l2 + l3), err
}

var _ api.MeterCurrent = (*ABLeMH)(nil)

// Currents implements the api.MeterCurrent interface
func (wb *ABLeMH) Currents() (float64, float64, float64, error) {
	_, _ = wb.conn.ReadHoldingRegisters(ablRegStatusLong, 5)
	b, err := wb.conn.ReadHoldingRegisters(ablRegStatusLong, 5)
	if err != nil {
		return 0, 0, 0, err
	}

	var currents []float64
	for i := 2; i < 5; i++ {
		u := binary.BigEndian.Uint16(b[2*i:])
		if u == ablAmpsDisabled {
			u = 0
		}

		currents = append(currents, float64(u)/10)
	}

	return currents[0], currents[1], currents[2], nil
}

var _ api.Diagnosis = (*ABLeMH)(nil)

// Diagnose implements the api.Diagnosis interface
func (wb *ABLeMH) Diagnose() {
	_, _ = wb.conn.ReadHoldingRegisters(ablRegFirmware, 2)
	b, err := wb.conn.ReadHoldingRegisters(ablRegFirmware, 2)
	if err == nil {
		fmt.Printf("Firmware: %0 x\n", b)
	}
}
