package pages

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func discoveryTester(s *viewState, bindable bool) *ui.Tester {
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Children(func() { s.discoveryView(c, bindable) })
	}, 1100, 2400)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	return tt
}

func compatibleFixture(name string) inferyard.DiscoveredService {
	return inferyard.DiscoveredService{PID: 3425, ProcessName: name, Endpoints: []inferyard.DiscoveredEndpoint{{
		URL: "http://127.0.0.1:3425", Health: ptr("4xx"), Models: []string{"/private/models/remote-model"},
	}}}
}

func TestDiscoveryCompatibleInterfacesRequireExpansion(t *testing.T) {
	for _, name := range []string{"magpie", "unknown-server", "python3.14"} {
		t.Run(name, func(t *testing.T) {
			s := testState()
			s.discoveryLoaded = true
			s.discovery.Services = []inferyard.DiscoveredService{compatibleFixture(name)}
			tt := discoveryTester(&s, true)
			for _, hidden := range []string{name, "remote-model", "PID 3425"} {
				if tt.HasText(hidden) {
					t.Fatalf("unconfirmed interface visible by default: %s; %v", hidden, tt.Texts())
				}
			}
			if !tt.HasText("暂无 Studio 启动的活动服务。") {
				t.Fatal(tt.Texts())
			}
			click(t, tt, "待确认的兼容接口")
			tt.Frame()
			for _, want := range []string{name, "PID 3425 · http://127.0.0.1:3425", "健康检查：4xx", "可能是代理", "绑定"} {
				if !tt.HasText(want) {
					t.Fatal(want, tt.Texts())
				}
			}
			if tt.HasText("remote-model") {
				t.Fatal("model directory expanded automatically", tt.Texts())
			}
			click(t, tt, "模型目录（1）")
			tt.Frame()
			if !tt.HasText("remote-model") || tt.HasText("/private/") {
				t.Fatal(tt.Texts())
			}
		})
	}
}

func TestDiscoveryManagedServiceIndependentOfScan(t *testing.T) {
	for _, state := range []string{"starting", "running", "stopping"} {
		for _, scan := range []string{"unloaded", "scanning", "failed", "partial", "missing-models"} {
			t.Run(state+"/"+scan, func(t *testing.T) {
				s := testState()
				s.model, s.engine = "selected-model", "selected-engine"
				s.engineState = core.EngineState{State: state, Pid: 42, Endpoint: "http://127.0.0.1:18957", Candidate: "/private/candidates/frozen-core"}
				switch scan {
				case "scanning":
					s.discovering = true
				case "failed":
					s.discoveryError = core.Err("service_discovery_error")
				case "partial":
					s.discoveryLoaded = true
					s.discovery.Status = "partial"
					s.discovery.Limitations = []string{"listener_table_incomplete"}
				case "missing-models":
					s.discoveryLoaded = true
					s.discovery.Services = []inferyard.DiscoveredService{{PID: 42, ProcessName: "fake-engine", Endpoints: []inferyard.DiscoveredEndpoint{{URL: s.engineState.Endpoint, Health: ptr("2xx")}}}}
				}
				tt := discoveryTester(&s, false)
				for _, want := range []string{"Studio 启动的服务", "状态：" + stateLabel(state), "候选：frozen-core", "PID 42", "http://127.0.0.1:18957"} {
					if !tt.HasText(want) {
						t.Fatal(want, tt.Texts())
					}
				}
				for _, hidden := range []string{"/private/", "selected-model", "selected-engine", "暂无 Studio", "未发现本机模型服务"} {
					if tt.HasText(hidden) {
						t.Fatal(hidden, tt.Texts())
					}
				}
			})
		}
	}
}

func TestDiscoveryLifecycleChangesAndDeduplication(t *testing.T) {
	s := testState()
	s.discoveryLoaded = true
	s.engineState = core.EngineState{State: "starting", Pid: 3425, Endpoint: "http://127.0.0.1:3425", Candidate: "/private/candidates/frozen-core"}
	s.discovery.Services = []inferyard.DiscoveredService{compatibleFixture("same-listener")}
	tt := discoveryTester(&s, false)
	for _, state := range []string{"starting", "running", "stopping", "stopped", "failed", "orphaned"} {
		s.engineState.State = state
		tt.Frame()
		active := state == "starting" || state == "running" || state == "stopping"
		if active {
			if !tt.HasText("状态："+stateLabel(state)) || tt.HasText("待确认的兼容接口") {
				t.Fatal(state, tt.Texts())
			}
		} else {
			if tt.HasText("Studio 启动的服务") || tt.HasText("状态：运行中") || !tt.HasText("待确认的兼容接口") {
				t.Fatal(state, tt.Texts())
			}
			if state == "orphaned" && !tt.HasText("遗留服务记录（状态未确认）") {
				t.Fatal(tt.Texts())
			}
		}
	}
}

func TestDiscoveryDedupRequiresActivePIDAndExactEndpoint(t *testing.T) {
	for _, scenario := range []string{"different-pid", "different-endpoint", "ambiguous", "no-pid", "stopped", "failed", "orphaned"} {
		t.Run(scenario, func(t *testing.T) {
			s := testState()
			s.engineState = core.EngineState{State: "running", Pid: 3425, Endpoint: "http://127.0.0.1:3425"}
			service := compatibleFixture("external-listener")
			switch scenario {
			case "different-pid":
				service.PID++
			case "different-endpoint":
				service.Endpoints[0].URL = "http://127.0.0.1:9999"
			case "ambiguous":
				service.Endpoints[0].AmbiguousListener = true
			case "no-pid":
				s.engineState.Pid = 0
			default:
				s.engineState.State = scenario
			}
			s.discovery.Services = []inferyard.DiscoveredService{service}
			tt := discoveryTester(&s, true)
			click(t, tt, "待确认的兼容接口")
			tt.Frame()
			if !tt.HasText("external-listener") || !tt.HasText(fmt.Sprintf("PID %d · %s", service.PID, service.Endpoints[0].URL)) {
				t.Fatal(tt.Texts())
			}
		})
	}
}

func TestDiscoveryEmptyAndTerminalStates(t *testing.T) {
	for _, state := range []string{"", "stopped", "failed"} {
		s := testState()
		s.engineState = core.EngineState{State: state, Pid: 42, Endpoint: "http://127.0.0.1:18957"}
		s.discoveryLoaded = true
		tt := discoveryTester(&s, false)
		if !tt.HasText("暂无 Studio 启动的活动服务。") || tt.HasText("PID 42") || tt.HasText("运行中") {
			t.Fatal(state, tt.Texts())
		}
		if !tt.HasText("未发现提供模型目录的兼容接口。") {
			t.Fatal(tt.Texts())
		}
	}
}

func TestDiscoveryBindingDisabledDuringBusyScanOrOtherPhase(t *testing.T) {
	for _, scenario := range []string{"busy", "scanning", "other-phase"} {
		t.Run(scenario, func(t *testing.T) {
			s := testState()
			s.discovery.Services = []inferyard.DiscoveredService{compatibleFixture("external-listener")}
			switch scenario {
			case "busy":
				s.busy = true
			case "scanning":
				s.discovering = true
			case "other-phase":
				s.progress.NextAction = "prepare_candidate"
			}
			tt := discoveryTester(&s, true)
			click(t, tt, "待确认的兼容接口")
			tt.Frame()
			// No service is installed: a forbidden click must never start binding work.
			click(t, tt, "绑定")
			if s.binding {
				t.Fatal("disabled action started binding", tt.Texts())
			}
		})
	}
}

func TestDiscoveryHealthyDirectoryStillUnconfirmed(t *testing.T) {
	s := testState()
	service := compatibleFixture("llama-server")
	service.Endpoints[0].Health = ptr("2xx")
	service.Endpoints[0].Models = []string{"local-model", "remote-model"}
	s.discovery.Services = []inferyard.DiscoveredService{service}
	tt := discoveryTester(&s, false)
	if tt.HasText("llama-server") || tt.HasText("local-model") || tt.HasText("PID 3425") {
		t.Fatal("health and directory granted service identity", tt.Texts())
	}
	click(t, tt, "待确认的兼容接口")
	tt.Frame()
	if !tt.HasText("llama-server") || !tt.HasText("健康检查：2xx") || tt.HasText("local-model") {
		t.Fatal(tt.Texts())
	}
}

func TestModelsDiscoveryTitleIsNotRepeated(t *testing.T) {
	s := testState()
	tt := ui.NewTester(s.modelsView, 1120, 2400)
	if got := len(modelUINodes(tt, "text", "本机运行的服务")); got != 1 {
		t.Fatalf("service card title count = %d: %s", got, strings.Join(tt.Texts(), " | "))
	}
}
