package inferyard

import "testing"

func TestValidCandidateSafetyRejectsTemperatureStopFieldsAndInvalidBounds(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{
			name: "accepts_environment_only_safety",
			raw:  `{"interval_seconds":1,"check_environment":true}`,
			want: true,
		},
		{
			name: "rejects_legacy_temperature_stop_fields",
			raw:  `{"interval_seconds":1,"max_temperature_celsius":85,"require_temperature":false,"check_environment":true}`,
			want: false,
		},
		{
			name: "rejects_interval_above_bound",
			raw:  `{"interval_seconds":6,"check_environment":true}`,
			want: false,
		},
		{
			name: "rejects_disabled_environment_check",
			raw:  `{"interval_seconds":1,"check_environment":false}`,
			want: false,
		},
	}
	for _, tc := range cases {
		if got := validCandidateSafety([]byte(tc.raw)); got != tc.want {
			t.Fatalf("%s: validCandidateSafety(%s) = %v, want %v", tc.name, tc.raw, got, tc.want)
		}
	}
}
