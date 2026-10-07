package observe

import (
	"math"
	"strings"
	"testing"
)

func TestTrackerRejectsReusedPIDPermanently(t *testing.T) {
	target := Target{42, 8, "model"}
	tracker := NewTracker()
	raw := RawProcess{target, Known(uint64(10), "native.cpu"), Known(uint64(20), "native.rss")}
	first := tracker.Read(target, 0, 2, raw, nil)
	if first.CPUPercent.Value != nil || *first.CPUPercent.Reason != "first_observation" {
		t.Fatal(first)
	}
	raw.CPUNS = Known(uint64(20), "native.cpu")
	next := tracker.Read(target, 10, 12, raw, nil)
	if next.CPUPercent.Value == nil || *next.CPUPercent.Value != 100 {
		t.Fatal(next)
	}
	raw.Target.StartTicks++
	lost := tracker.Read(target, 20, 22, raw, nil)
	if lost.State != "identity_changed" || lost.RSS.Value != nil {
		t.Fatal(lost)
	}
	raw.Target = target
	if tracker.Read(target, 30, 32, raw, nil).State != "identity_changed" {
		t.Fatal("rebound lost PID")
	}
}

func TestTrackerCounterGapsAndPermission(t *testing.T) {
	target := Target{1, 2, "model"}
	tr := NewTracker()
	raw := RawProcess{target, Known(uint64(20), "native.cpu"), Known(uint64(2), "native.rss")}
	tr.Read(target, 0, 2, raw, nil)
	raw.CPUNS = Known(uint64(19), "native.cpu")
	if p := tr.Read(target, 10, 12, raw, nil); p.CPUPercent.Value != nil || *p.CPUPercent.Reason != "counter_or_clock_regressed" {
		t.Fatal(p)
	}
	raw.CPUNS = Missing[uint64]("permission_denied", "native.cpu")
	tr.Read(target, 20, 22, raw, nil)
	raw.CPUNS = Known(uint64(30), "native.cpu")
	if p := tr.Read(target, 30, 32, raw, nil); *p.CPUPercent.Reason != "first_observation" {
		t.Fatal(p)
	}
	if p := tr.Read(target, 30, 32, raw, nil); *p.CPUPercent.Reason != "counter_or_clock_regressed" {
		t.Fatal(p)
	}
	if p := NewTracker().Read(target, 0, 1, RawProcess{}, ErrPermission); p.State != "unavailable" || *p.RSS.Reason != "permission_denied" {
		t.Fatal(p)
	}
	if p := NewTracker().Read(target, 0, 1, RawProcess{}, ErrExited); p.State != "exited" {
		t.Fatal(p)
	}
}

func statText(state, user, system, start string) string {
	f := strings.Fields(state + " 0 0 0 0 0 0 0 0 0 0 " + user + " " + system + " 0 0 0 0 0 0 " + start)
	return "42 (name with ) spaces) " + strings.Join(f, " ")
}

func TestLinuxFieldUnitsAndMalformedCounters(t *testing.T) {
	stat, err := parseProcStat(statText("S", "7", "8", "123"))
	if err != nil || stat.cpu != 15 || stat.start != 123 || stat.zombie {
		t.Fatal(stat, err)
	}
	stat, err = parseProcStat(statText("Z", "7", "8", "123"))
	if err != nil || !stat.zombie {
		t.Fatal(stat, err)
	}
	for _, text := range []string{"42 no close", "42 (x) S", statText("S", "-1", "0", "1"), statText("S", "1", "0", "0"), statText("S", "18446744073709551615", "1", "1")} {
		if _, err := parseProcStat(text); err == nil {
			t.Fatal(text)
		}
	}
	if value, err := parseMemValue("MemTotal: 16 kB\nMemAvailable: 3 kB\n", "MemAvailable"); err != nil || value != 3072 {
		t.Fatal(value, err)
	}
	for _, text := range []string{"MemAvailable: 4 MB", "MemAvailable: -1 kB", "MemAvailable: 18446744073709551615 kB", "Other: 2 kB"} {
		if _, err := parseMemValue(text, "MemAvailable"); err == nil {
			t.Fatal(text)
		}
	}
	if ns, err := ticksNS(3, 128); err != nil || ns != 23_437_500 {
		t.Fatal(ns, err)
	}
	for _, pair := range [][2]uint64{{1, 0}, {1, 1_000_000_001}, {math.MaxUint64, 1}} {
		if _, err := ticksNS(pair[0], pair[1]); err == nil {
			t.Fatal(pair)
		}
	}
	if !IsCandidate("LLAMA-SERVER.EXE") || IsCandidate("llama-server-copy") {
		t.Fatal("candidate exact name")
	}
}
