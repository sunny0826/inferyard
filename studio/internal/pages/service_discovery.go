package pages

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/inferyard"
)

func (s *viewState) refreshDiscovery() {
	if s.discovering {
		return
	}
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

// modelEndpoints returns the endpoints that identified themselves as model
// services: the CLI only reports a model list for OpenAI-compatible
// endpoints, so every other loopback listener (chat apps, proxies, dev
// servers) is noise for the binding list.
func modelEndpoints(service inferyard.DiscoveredService) []inferyard.DiscoveredEndpoint {
	endpoints := []inferyard.DiscoveredEndpoint{}
	for _, endpoint := range service.Endpoints {
		if endpoint.Models != nil {
			endpoints = append(endpoints, endpoint)
		}
	}
	return endpoints
}

// discoveryView renders the discovered model services. bindable shows
// per-service bind actions (Dashboard only); the models page renders the same
// list read-only.
func (s *viewState) discoveryView(c *ui.Context, bindable bool) {
	heading(c, "本机运行的服务", 3)
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
	modelServices, other := 0, 0
	for _, service := range s.discovery.Services {
		if len(modelEndpoints(service)) > 0 {
			modelServices++
		} else {
			other++
		}
	}
	if modelServices == 0 && s.discoveryError == nil && !s.discovering {
		message := "尚未读取本机服务。"
		if s.discoveryLoaded {
			message = "未发现本机模型服务。"
			if len(s.discovery.Limitations) > 0 {
				message = "扫描不完整，未发现本机模型服务。"
			}
		}
		ui.Text(c, message)
	}
	for _, service := range s.discovery.Services {
		for _, endpoint := range modelEndpoints(service) {
			ui.Column(c.Key(fmt.Sprintf("service-%d-%s", service.PID, endpoint.URL))).Gap(c.Theme().Space(2)).Children(func() {
				models := []string{}
				for _, model := range endpoint.Models {
					models = append(models, displayName(model))
				}
				name := displayName(service.ProcessName)
				if len(models) > 0 {
					name = strings.Join(models, "、") + " · " + name
				}
				heading(c, name, 3)
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
				// Discovery results never decide whether CLI bind will accept a service.
				if bindable {
					button(c, "绑定").Disabled(s.busy || s.discovering || !s.bindingPhase()).OnClick(func() {
						s.bindDiscovered(service.PID, endpoint.URL)
					})
				}
			})
		}
	}
	if other > 0 && s.discoveryError == nil {
		ui.Text(c, fmt.Sprintf("另有 %d 个未识别为模型服务的进程未列出。", other)).TextColor(c.Theme().TextMuted)
	}
}
