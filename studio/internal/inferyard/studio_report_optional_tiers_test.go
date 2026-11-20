package inferyard

import "testing"

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
