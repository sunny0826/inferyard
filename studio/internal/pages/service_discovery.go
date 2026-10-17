package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/inferyard"
)

func (s *viewState) refreshDiscovery() {
	if s.simulate(previewRefreshDiscovery) {
		return
	}
	if s.discovering {
		return
	}
	s.engineState = s.service.Engine()
	s.discovering = true
	cliKey := s.discoveryCLI
	go func() {
		result, e := s.service.Discover(s.ctx)
		if s.ctx.Err() != nil {
			return
		}
		s.win.Update(func() {
			if cliKey != s.discoveryCLI {
				s.discovering = false
				s.refreshDiscovery()
				return
			}
			s.discovery, s.discoveryError = result, e
			s.discovering, s.discoveryLoaded = false, true
		})
	}()
}

func (s *viewState) managedServiceActive() bool {
	switch s.engineState.State {
	case "starting", "running", "stopping":
		return true
	default:
		return false
	}
}

// Only suppress a duplicate display; discovery never grants process ownership.
func (s *viewState) managedEndpoint(service inferyard.DiscoveredService, endpoint inferyard.DiscoveredEndpoint) bool {
	return s.managedServiceActive() && s.engineState.Pid > 0 && service.PID == s.engineState.Pid &&
		s.engineState.Endpoint != "" && endpoint.URL == s.engineState.Endpoint && !endpoint.AmbiguousListener
}

func (s *viewState) managedServiceView(c *ui.Context) {
	state := s.engineState
	switch {
	case s.managedServiceActive():
		heading(c, "Studio 启动的服务", 3)
		ui.Text(c, "状态："+stateLabel(state.State))
	case state.State == "orphaned":
		heading(c, "遗留服务记录（状态未确认）", 3)
		ui.Text(c, "上次 Studio 留下的服务或记录，尚不能确认运行状态。").TextColor(c.Theme().Warning)
	default:
		ui.Text(c, "暂无 Studio 启动的活动服务。")
		return
	}
	if state.Candidate != "" {
		ui.Text(c, "候选："+displayName(state.Candidate))
	}
	if state.Pid > 0 {
		ui.Text(c, fmt.Sprintf("PID %d", state.Pid))
	}
	if state.Endpoint != "" {
		ui.Text(c, loopbackEndpoint(state.Endpoint))
	}
}

// The Models card supplies its own title. Only Dashboard exposes binding.
func (s *viewState) discoveryView(c *ui.Context, bindable bool) {
	if bindable {
		heading(c, "本机运行的服务", 3)
	}
	s.managedServiceView(c)
	button(c, "刷新服务列表").Disabled(s.discovering || s.busy).OnClick(s.refreshDiscovery)
	if s.discovering {
		ui.Text(c, "正在发现本机服务…")
	}
	errorView(c, s.discoveryError)
	if s.discoveryError == nil {
		for _, code := range s.discovery.Limitations {
			ui.Text(c, human(code)+" · "+code).TextColor(c.Theme().Warning)
		}
	}
	compatible, other := 0, 0
	for _, service := range s.discovery.Services {
		listed := false
		for _, endpoint := range service.Endpoints {
			if endpoint.Models != nil || s.managedEndpoint(service, endpoint) {
				listed = true
			}
			if endpoint.Models != nil && !s.managedEndpoint(service, endpoint) {
				compatible++
			}
		}
		if !listed {
			other++
		}
	}
	if compatible > 0 {
		componentFold(c, "待确认的兼容接口", func() {
			ui.Text(c, "这些接口可能是代理；模型目录不证明本机已加载模型。")
			for _, service := range s.discovery.Services {
				for _, endpoint := range service.Endpoints {
					if endpoint.Models != nil && !s.managedEndpoint(service, endpoint) {
						s.compatibleEndpointView(c, service, endpoint, bindable)
					}
				}
			}
		})
	} else if s.discoveryError == nil && !s.discovering {
		message := "尚未读取本机服务。"
		if s.discoveryLoaded {
			message = "未发现提供模型目录的兼容接口。"
			if s.managedServiceActive() {
				message = "未发现其他提供模型目录的兼容接口。"
			}
			if s.discovery.Status == "partial" || len(s.discovery.Limitations) > 0 {
				message = "扫描不完整，兼容接口列表可能有遗漏。"
			}
		}
		ui.Text(c, message)
	}
	if other > 0 && s.discoveryError == nil {
		ui.Text(c, fmt.Sprintf("另有 %d 个未提供模型目录的进程未列出。", other)).TextColor(c.Theme().TextMuted)
	}
}

func (s *viewState) compatibleEndpointView(c *ui.Context, service inferyard.DiscoveredService, endpoint inferyard.DiscoveredEndpoint, bindable bool) {
	ui.Column(c.Key(fmt.Sprintf("service-%d-%s", service.PID, endpoint.URL))).Gap(c.Theme().Space(2)).Children(func() {
		heading(c, displayName(service.ProcessName), 3)
		ui.Text(c, fmt.Sprintf("PID %d · %s", service.PID, endpoint.URL))
		if endpoint.Health == nil {
			ui.Text(c, "健康检查：缺测")
		} else {
			ui.Text(c, "健康检查："+*endpoint.Health)
		}
		if endpoint.AmbiguousListener {
			ui.Text(c, "多个进程监听同一端口，归属存在歧义 · ambiguous_listener").TextColor(c.Theme().Warning)
		}
		if endpoint.Reason != nil {
			ui.Text(c, human(*endpoint.Reason)+" · "+*endpoint.Reason)
		}
		componentFold(c, fmt.Sprintf("模型目录（%d）", len(endpoint.Models)), func() {
			for _, model := range endpoint.Models {
				ui.Text(c, displayName(model))
			}
		})
		if bindable {
			button(c, "绑定").Disabled(s.busy || s.discovering || !s.bindingPhase()).OnClick(func() {
				s.bindDiscovered(service.PID, endpoint.URL)
			})
		}
	})
}
