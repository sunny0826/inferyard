package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// Each page keeps its own scroll place: switching away and back restores
// the offset the page was left at, instead of jumping back to the top.
func TestEachPageKeepsItsOwnScrollPosition(t *testing.T) {
	s := testState()
	s.selected = "settings"
	tt := ui.NewTester(s.view, 1120, 800)

	tt.Scroll(700, 500, 0, 600)
	if got := s.pageScroll().Y; got <= 0 {
		t.Fatalf("settings page did not scroll: %v", got)
	}
	scrolled := s.pageScroll().Y

	s.selected = "device"
	tt.Frame()
	if got := s.pageScroll().Y; got != 0 {
		t.Fatalf("a fresh page did not start at the top: %v", got)
	}
	s.selected = "settings"
	tt.Frame()
	if got := s.pageScroll().Y; got != scrolled {
		t.Fatalf("settings lost its scroll place: %v, want %v", got, scrolled)
	}
}

// The keyed scroll container keeps the page's own scroll state, so the
// offset survives the page switch that rebuilds the tree.
func TestScrollStateFollowsTheSelectedPage(t *testing.T) {
	s := testState()
	s.selected = "settings"
	tt := ui.NewTester(s.view, 1120, 800)
	tt.Scroll(700, 500, 0, 600)
	settingsOffset := s.pageScroll().Y
	if settingsOffset <= 0 {
		t.Fatalf("settings page did not scroll: %v", settingsOffset)
	}

	s.selected = "reports"
	tt.Frame()
	tt.Scroll(700, 500, 0, 300)
	otherOffset := s.pageScroll().Y
	if otherOffset <= 0 {
		t.Fatalf("reports page did not scroll: %v", otherOffset)
	}
	if otherOffset == settingsOffset {
		t.Fatal("pages share one scroll state")
	}

	s.selected = "settings"
	tt.Frame()
	if got := s.pageScroll().Y; got != settingsOffset {
		t.Fatalf("settings lost its scroll place: %v, want %v", got, settingsOffset)
	}
	s.selected = "reports"
	tt.Frame()
	if got := s.pageScroll().Y; got != otherOffset {
		t.Fatalf("reports lost its scroll place: %v, want %v", got, otherOffset)
	}
}

// A report opened from the list keeps its own place, apart from the list.
func TestOpenedReportScrollIsSeparateFromTheList(t *testing.T) {
	s := testState()
	s.service, _ = reportFixtureService(t)
	s.selected = "reports"
	tt := ui.NewTester(s.view, 1120, 800)
	tt.Scroll(700, 500, 0, 400)
	listOffset := s.pageScroll().Y
	if listOffset <= 0 {
		t.Fatalf("report list did not scroll: %v", listOffset)
	}
	s.openReport("summary-1")
	tt.Frame()
	if got := s.pageScroll().Y; got != 0 {
		t.Fatalf("an opened report did not start at the top: %v", got)
	}
}
