package inferyard

import (
	"context"
	"encoding/json"
	"testing"
)

const validResult = `{"command":"device-check","status":"inspected","completeness":"complete","run_id":null,"evidence_dir":null,"limitations":[],"details":{"kind":"device_preflight.v1","hardware":{"cpu":{"name":"fixture"}},"models":[],"recommendation":{"status":"blocked"},"platform_capabilities":{}}}`

func TestParse(t *testing.T) {
	result, err := Parse([]byte(validResult + "\n"))
	if err != nil || result.RunID != nil {
		t.Fatalf("%+v %v", result, err)
	}
	device, err := ParseDeviceCheck(result)
	if err != nil || !json.Valid(device.Hardware) {
		t.Fatalf("%+v %v", device, err)
	}
	for _, bad := range []string{"", "null", "[]", "{}", validResult + "{}", validResult + "garbage", `{"command":3}`, "{\xff}"} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	result.Details = json.RawMessage(`{"kind":"device_preflight.v2"}`)
	if _, err := ParseDeviceCheck(result); err == nil {
		t.Fatal("unsupported kind accepted")
	}
}
func TestDeviceCheck(t *testing.T) {
	path := script(t, "[ \"$1\" = device-check ] && [ \"$2\" = --json ] || exit 4\nprintf '%s' '"+validResult+"'")
	device, out, err := (Client{Path: path}).DeviceCheck(context.Background())
	if err != nil || device.Kind != "device_preflight.v1" || out.Status != Complete {
		t.Fatalf("%+v %+v %v", device, out, err)
	}
}
