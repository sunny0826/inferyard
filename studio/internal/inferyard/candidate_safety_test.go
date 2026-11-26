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
			name: "accepts_check_environment_false",
			raw:  `{"interval_seconds":1,"check_environment":false}`,
			want: true,
		},
	}
	for _, tc := range cases {
		if got := validCandidateSafety([]byte(tc.raw)); got != tc.want {
			t.Fatalf("%s: validCandidateSafety(%s) = %v, want %v", tc.name, tc.raw, got, tc.want)
		}
	}
}

func TestValidCandidateEnvironmentAllowsCustomVarsAndRejectsInvalid(t *testing.T) {
	if !validCandidateEnvironment(map[string]string{
		"HF_ENDPOINT":          "https://hf-mirror.com",
		"CUDA_VISIBLE_DEVICES": "0,1",
	}) {
		t.Fatal("expected custom environment variables to be accepted")
	}
	if validCandidateEnvironment(map[string]string{"BAD-KEY": "1"}) {
		t.Fatal("expected invalid variable name to be rejected")
	}
	if validCandidateEnvironment(map[string]string{"VALID_KEY": "bad\x00val"}) {
		t.Fatal("expected NUL byte in value to be rejected")
	}
}
