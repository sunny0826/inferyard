package pages

import "testing"

func TestLegacyPageIDRedirects(t *testing.T) {
	tests := []struct {
		name           string
		legacy         string
		wantPage       string
		wantLibraryTab int
		wantHwExpanded bool
	}{
		{
			name:     "dashboard_to_benchmarks",
			legacy:   "dashboard",
			wantPage: "benchmarks",
		},
		{
			name:           "models_to_library_models_tab",
			legacy:         "models",
			wantPage:       "library",
			wantLibraryTab: 0,
		},
		{
			name:           "downloads_to_library",
			legacy:         "downloads",
			wantPage:       "library",
			wantLibraryTab: 0,
		},
		{
			name:           "device_to_library_with_hw_expanded",
			legacy:         "device",
			wantPage:       "library",
			wantHwExpanded: true,
		},
		{
			name:     "leading_slash_dashboard",
			legacy:   "/dashboard",
			wantPage: "benchmarks",
		},
		{
			name:     "unknown_defaults_to_services",
			legacy:   "unknown-page",
			wantPage: "services",
		},
	}

	for _, tc := range tests {
		t.Run("navigateToNow/"+tc.name, func(t *testing.T) {
			s := readyState("services")
			s.libraryTab = 1
			s.libraryHwExpanded = false
			s.navigateToNow(tc.legacy, "")
			if s.selected != tc.wantPage {
				t.Fatalf("navigateToNow(%q): got selected=%q, want %q", tc.legacy, s.selected, tc.wantPage)
			}
			if tc.wantPage == "library" && tc.legacy != "device" && s.libraryTab != tc.wantLibraryTab {
				t.Fatalf("navigateToNow(%q): got libraryTab=%d, want %d", tc.legacy, s.libraryTab, tc.wantLibraryTab)
			}
			if tc.wantHwExpanded && !s.libraryHwExpanded {
				t.Fatalf("navigateToNow(%q): expected libraryHwExpanded=true", tc.legacy)
			}
		})

		t.Run("selectPageNow/"+tc.name, func(t *testing.T) {
			s := readyState("services")
			s.libraryTab = 1
			s.libraryHwExpanded = false
			s.selectPageNow(tc.legacy)
			if s.selected != tc.wantPage {
				t.Fatalf("selectPageNow(%q): got selected=%q, want %q", tc.legacy, s.selected, tc.wantPage)
			}
			if tc.wantPage == "library" && tc.legacy != "device" && s.libraryTab != tc.wantLibraryTab {
				t.Fatalf("selectPageNow(%q): got libraryTab=%d, want %d", tc.legacy, s.libraryTab, tc.wantLibraryTab)
			}
			if tc.wantHwExpanded && !s.libraryHwExpanded {
				t.Fatalf("selectPageNow(%q): expected libraryHwExpanded=true", tc.legacy)
			}
		})
	}
}

func TestFirstOpenDefaultsToServices(t *testing.T) {
	if got := firstOpen("", false); got != "services" {
		t.Fatalf("firstOpen(\"\", false) = %q, want \"services\"", got)
	}
	if got := firstOpen("", true); got != "services" {
		t.Fatalf("firstOpen(\"\", true) = %q, want \"services\"", got)
	}
	if got := firstOpen("/dashboard", false); got != "benchmarks" {
		t.Fatalf("firstOpen(\"/dashboard\", false) = %q, want \"benchmarks\"", got)
	}
	if got := firstOpen("/device", false); got != "library" {
		t.Fatalf("firstOpen(\"/device\", false) = %q, want \"library\"", got)
	}
}
