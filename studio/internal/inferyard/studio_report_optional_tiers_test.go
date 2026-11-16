package inferyard

import "testing"

// studioIntervalTier is one sample-count boundary case for the interval rules.
type studioIntervalTier struct {
	name      string
	count     int64
	p95       string
	p95Reason string
}

func (tier studioIntervalTier) document(t *testing.T) []byte {
	t.Helper()
	section := studioIntervalsAt(t, tier.count, tier.p95, tier.p95Reason)
	return studioPatch(t, studioReportFixture, studioDashboardPath("output_block_intervals"), studioBytes(t, section))
}

func TestParseStudioReportAcceptsIntervalSampleTiers(t *testing.T) {
	tiers := []studioIntervalTier{
		{"zero samples", 0, "null", `"insufficient_samples"`},
		{"one sample", 1, "null", `"insufficient_samples"`},
		{"nineteen samples", 19, "null", `"insufficient_samples"`},
		{"twenty samples", 20, "400", "null"},
	}
	for _, tier := range tiers {
		t.Run(tier.name, func(t *testing.T) {
			if _, err := ParseStudioReport(tier.document(t)); err != nil {
				t.Fatalf("valid %s rejected: %v", tier.name, err)
			}
		})
	}
}

func TestParseStudioReportRejectsIntervalSampleTiers(t *testing.T) {
	tiers := []studioIntervalTier{
		{"zero samples with an arbitrary p95 reason", 0, "null", `"arbitrary"`},
		{"zero samples without a p95 reason", 0, "null", "null"},
		{"one sample with an arbitrary p95 reason", 1, "null", `"arbitrary"`},
		{"nineteen samples with a p95 value", 19, "400", `"insufficient_samples"`},
		{"twenty samples without p95", 20, "null", `"insufficient_samples"`},
		{"twenty samples with a p95 reason", 20, "400", `"insufficient_samples"`},
	}
	for _, tier := range tiers {
		t.Run(tier.name, func(t *testing.T) {
			if _, err := ParseStudioReport(tier.document(t)); err == nil {
				t.Fatalf("broken %s accepted", tier.name)
			}
		})
	}
}
