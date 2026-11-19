package pages

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

// A chain over two unbound bundles reports the bundle whose connection failed,
// not the first one it saw, and stops there.
func TestBindingChainReportsFailingBundle(t *testing.T) {
	frames := []inferyard.WorkspaceProgress{
		{Workspace: "/w", NextAction: "bind", Bundles: []inferyard.BundleProgress{bindable("zh-core", "/w/c/zh-core.toml")}},
		{Workspace: "/w", NextAction: "bind", Bundles: []inferyard.BundleProgress{bindable("zh-svg-pelican", "/w/c/zh-svg-pelican.toml")}},
	}
	var ran []string
	var announced []string
	failed, e := runBindingChain(context.Background(), "/w",
		[]bindingJob{{"zh-core", "/w/c/zh-core.toml"}, {"zh-svg-pelican", "/w/c/zh-svg-pelican.toml"}},
		4001, "http://127.0.0.1:48857",
		func(context.Context) (core.ProgressResponse, *core.ApiError) {
			if len(frames) == 0 {
				return core.ProgressResponse{}, nil
			}
			f := frames[0]
			frames = frames[1:]
			return core.ProgressResponse{Progress: &f}, nil
		},
		func(_ context.Context, q core.WizardRequest) (core.WizardResult, *core.ApiError) {
			ran = append(ran, q.Command)
			if len(ran) == 2 {
				return core.WizardResult{}, core.Err("service_identity_unavailable")
			}
			return core.WizardResult{}, nil
		},
		func(bundle string) { announced = append(announced, bundle) },
	)
	if e == nil || e.Code != "service_identity_unavailable" {
		t.Fatalf("chain result: %+v", e)
	}
	if failed != "zh-svg-pelican" {
		t.Fatalf("failing bundle: %q (ran %v)", failed, ran)
	}
	if len(announced) != 2 || announced[1] != "zh-svg-pelican" {
		t.Fatalf("announcements: %v", announced)
	}
}

// bindable is the progress row the chain requires before it derives a bind command.
func bindable(bundle, candidate string) inferyard.BundleProgress {
	return inferyard.BundleProgress{
		Bundle: bundle, Phase: "bound_missing", NextAction: "bind",
		CandidatePath: ptr(candidate),
		NextCommand:   ptr("inferyard config bind --candidate " + candidate + " --pid 1 --endpoint http://127.0.0.1:1"),
	}
}

// A finished connection records the service only when it was accepted, and the
// rejection keeps the bundle and the cause together.
func TestApplyBindingRecordsServiceOnlyOnSuccess(t *testing.T) {
	s := bindingState(nil)
	s.applyBinding("zh-svg-pelican", core.Err("service_process_unavailable"), 4001)
	if s.localPIDUsed(4001) {
		t.Fatal("rejected connection consumed the service")
	}
	if s.failedBundle != "zh-svg-pelican" || s.bindFailure == nil {
		t.Fatalf("rejection lost: %q %+v", s.failedBundle, s.bindFailure)
	}
	s.applyBinding("", nil, 4001)
	if !s.localPIDUsed(4001) {
		t.Fatal("accepted connection not recorded")
	}
	if s.failedBundle != "" || s.bindFailure != nil {
		t.Fatalf("success kept the rejection: %+v", s.bindFailure)
	}
}

// The bind step renders the rejection once: the page banner does not repeat it.
func TestDiscoveredBindingFailureRenderedOnce(t *testing.T) {
	s := bindingState(nil)
	s.progress.NextAction = "bind"
	s.applyBinding("zh-svg-pelican", core.Err("service_process_unavailable"), 4001)
	tt := ui.NewTester(s.view, 1120, 1800)
	if n := countText(tt, "service_process_unavailable"); n != 1 {
		t.Fatalf("cause rendered %d times: %v", n, tt.Texts())
	}
}
