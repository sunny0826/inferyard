package inferyard

import "testing"

func TestValidCandidateSafetyTemperatureCeilings(t *testing.T) {
	cases := []struct {
		name   string
		family string
		raw    string
		want   bool
	}{
		{
			name:   "metal_accepts_115",
			family: PolystrataFamily,
			raw:    `{"interval_seconds":1,"max_temperature_celsius":115,"require_temperature":true,"check_environment":true}`,
			want:   true,
		},
		{
			name:   "metal_rejects_116",
			family: PolystrataFamily,
			raw:    `{"interval_seconds":1,"max_temperature_celsius":116,"require_temperature":true,"check_environment":true}`,
			want:   false,
		},
		{
			name:   "non_metal_accepts_85",
			family: PolystrataFamily,
			raw:    `{"interval_seconds":1,"max_temperature_celsius":85,"require_temperature":false,"check_environment":true}`,
			want:   true,
		},
		{
			name:   "non_metal_rejects_86",
			family: PolystrataFamily,
			raw:    `{"interval_seconds":1,"max_temperature_celsius":86,"require_temperature":false,"check_environment":true}`,
			want:   false,
		},
	}
	for _, tc := range cases {
		if got := validCandidateSafety([]byte(tc.raw), tc.family); got != tc.want {
			t.Fatalf("%s: validCandidateSafety(%s) = %v, want %v", tc.name, tc.raw, got, tc.want)
		}
	}
}
