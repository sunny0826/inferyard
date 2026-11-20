package pages

import (
	"testing"
	"time"
)

func TestTimeFormattingRoundsToTwoDecimals(t *testing.T) {
	ms := 1234.56789
	if got := formatDurationMs(&ms); got != "1.23 s" {
		t.Fatalf("formatDurationMs(1234.56789) = %q, want 1.23 s", got)
	}
	short := 840.0
	if got := formatDurationMs(&short); got != "840 ms" {
		t.Fatalf("formatDurationMs(840) = %q, want 840 ms", got)
	}
	if got := formatDurationMs(nil); got != "缺测" {
		t.Fatalf("formatDurationMs(nil) = %q, want 缺测", got)
	}
	if got := reportMilliseconds("", &ms, ""); got != "1234.57 ms" {
		t.Fatalf("reportMilliseconds = %q, want 1234.57 ms", got)
	}
	if got := formatDuration(12345 * time.Millisecond); got != "12.35 秒" {
		t.Fatalf("formatDuration(12.345s) = %q, want 12.35 秒", got)
	}
	if got := formatDuration(95 * time.Second); got != "1 分 35 秒" {
		t.Fatalf("formatDuration(95s) = %q, want 1 分 35 秒", got)
	}
	const raw = "2026-10-18T12:34:56.789123456Z"
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := formatTimestamp(raw), parsed.Local().Format("2006-01-02 15:04:05"); got != want {
		t.Fatalf("formatTimestamp = %q, want local %q", got, want)
	}
	if got := formatTimestampUTC(raw); got != "2026-10-18T12:34:56Z" {
		t.Fatalf("formatTimestampUTC = %q, want 2026-10-18T12:34:56Z", got)
	}
}

func TestConcentricRadiusComputation(t *testing.T) {
	if got := concentricRadius(12, 9); got != 3 {
		t.Fatalf("concentricRadius(12, 9) = %v, want 3", got)
	}
	if got := concentricRadius(6, 10); got != 2 {
		t.Fatalf("concentricRadius(6, 10) = %v, want floor 2", got)
	}
}
