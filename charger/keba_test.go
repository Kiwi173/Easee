package charger

import (
	"testing"

	"github.com/evcc-io/evcc/api"
)

func TestKeba(t *testing.T) {
	var wb api.Charger
	wb, err := NewKeba("localhost", "bar", RFID{}, 0)
	if err != nil {
		t.Error(err)
	}

	if _, ok := wb.(api.MeterEnergy); !ok {
		t.Error("missing MeterEnergy interface")
	}

	if _, ok := wb.(api.MeterCurrent); !ok {
		t.Error("missing MeterCurrent interface")
	}

	if _, ok := wb.(api.ChargeRater); !ok {
		t.Error("missing ChargeRater interface")
	}
}
