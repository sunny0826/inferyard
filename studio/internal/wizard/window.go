// Package wizard guides preparation using workspace evidence and native MyGo views.
package wizard

import (
	"context"
	"fmt"
	"log"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

// Capture returns a screenshot of the wizard window, for verification runs.
func (m *Manager) Capture() ([]byte, error) {
	m.mu.Lock()
	win := m.window
	m.mu.Unlock()
	if win == nil {
		return nil, fmt.Errorf("wizard window not open")
	}
	return win.CapturePage()
}

type WizardBridge struct{ manager *Manager }

func NewBridge(m *Manager) *WizardBridge { return &WizardBridge{manager: m} }
func (b *WizardBridge) Open()            { b.manager.Open() }

type Manager struct {
	ctx         context.Context
	service     *core.Service
	main        *mygo.Window
	mainFactory func(route string)
	mu          sync.Mutex
	window      *mygo.Window
}

func New(ctx context.Context, s *core.Service) *Manager {
	return &Manager{ctx: ctx, service: s}
}

// SetMain records the main window once it exists at app-ready time; the bridge
// itself must be bound at the top level so `mygo generate` sees the service.
func (m *Manager) SetMain(main *mygo.Window) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.main = main
}

// SetMainFactory records how to (re)create the main web window at a route. The
// wizard is the only window during onboarding; the main window is created on
// demand when the user asks for it and after the wizard closes.
func (m *Manager) SetMainFactory(fn func(route string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mainFactory = fn
}
func (m *Manager) OpenIfNeeded() {
	go func() {
		r := m.service.Readiness()
		p, e := m.service.Progress(m.ctx)
		if r.CLI.Status != "ok" || e != nil || p.Progress == nil || !p.Progress.DefaultScopeComplete {
			m.Open()
		}
	}()
}
func (m *Manager) Open() {
	// RunOnMain works with or without an existing main window; the wizard can
	// be the app's only window during onboarding.
	mygo.RunOnMain(func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.window != nil {
			m.window.Focus()
			return
		}
		ctx, cancel := context.WithCancel(m.ctx)
		w := &viewState{ctx: ctx, service: m.service, endpoint: "http://127.0.0.1:48857", openMain: m.openMain}
		win := mygo.NewWindow(mygo.WindowOptions{Title: "InferYard · 首次上手", Width: 1000, Height: 740, Content: ui.View(w.view)})
		w.win = win
		m.window = win
		win.OnClosed(func() {
			cancel()
			m.mu.Lock()
			m.window = nil
			main, factory := m.main, m.mainFactory
			m.mu.Unlock()
			// Keep one window alive: recreate the main window when the wizard
			// was the only one.
			if (main == nil || main.IsDestroyed()) && factory != nil {
				factory("")
			}
		})
		wake, off, _ := m.service.Bus.Subscribe()
		go func() {
			defer off()
			w.refresh()
			for {
				select {
				case <-ctx.Done():
					return
				case <-wake:
					w.refresh()
				}
			}
		}()
	})
}

// openMain focuses the main window and navigates it to a hash route, creating
// it first when it does not exist yet (onboarding startup or user-closed).
func (m *Manager) openMain(route string) {
	m.mu.Lock()
	main, factory := m.main, m.mainFactory
	m.mu.Unlock()
	if main == nil || main.IsDestroyed() {
		if factory == nil {
			log.Printf("wizard: main window unavailable and no factory installed")
			return
		}
		factory(route)
		return
	}
	main.Focus()
	if _, err := main.Page().Eval("location.hash = '" + route + "'"); err != nil {
		log.Printf("wizard: main window navigation to %s failed: %v", route, err)
	}
}

type viewState struct {
	refreshMu                                                         sync.Mutex
	ctx                                                               context.Context
	service                                                           *core.Service
	win                                                               *mygo.Window
	openMain                                                          func(string)
	ready                                                             core.Readiness
	progress                                                          *inferyard.WorkspaceProgress
	startup                                                           core.Startup
	processes                                                         []core.ProcessCandidate
	tasks                                                             []core.RunTask
	downloads                                                         []core.DownloadTask
	runProgress                                                       *core.RunProgress
	busy                                                              bool
	loaded                                                            bool
	message                                                           string
	failure                                                           string
	initDir, engine, modelURL, engineURL, tokenEnv, process, endpoint string
	binding                                                           bool
	candidate                                                         string
}

func text(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func fresh(root, name string) string {
	return filepath.Join(root, fmt.Sprintf("%s-%d", name, time.Now().UnixNano()))
}
func (w *viewState) active() *inferyard.BundleProgress {
	if w.progress != nil {
		for i := range w.progress.Bundles {
			if w.progress.Bundles[i].Phase != "run_complete" {
				return &w.progress.Bundles[i]
			}
		}
	}
	return nil
}
func terminal(s string) bool { return s == "done" || s == "failed" || s == "cancelled" }
func (w *viewState) running() *core.RunTask {
	for i := range w.tasks {
		if !terminal(w.tasks[i].State) {
			return &w.tasks[i]
		}
	}
	return nil
}
func (w *viewState) refresh() {
	w.refreshMu.Lock()
	defer w.refreshMu.Unlock()
	if w.ctx.Err() != nil {
		return
	}
	r := w.service.Readiness()
	var p core.ProgressResponse
	var e *core.ApiError
	if r.CLI.Status == "ok" {
		p, e = w.service.Progress(w.ctx)
	}
	tasks, downloads := w.service.RunTasks(), w.service.Downloads()
	var startup core.Startup
	var processes []core.ProcessCandidate
	candidate := ""
	if p.Progress != nil {
		for _, b := range p.Progress.Bundles {
			if b.Phase == "bound_missing" {
				candidate = text(b.CandidatePath)
				startup, e = w.service.Startup(candidate)
				if e == nil {
					processes, e = w.service.Processes(w.ctx)
				}
				break
			}
		}
	}
	var rp *core.RunProgress
	for _, t := range tasks {
		if !terminal(t.State) {
			if d, err := w.service.RunDetail(t.TaskId); err == nil {
				rp = d.Progress
			}
			break
		}
	}
	if w.ctx.Err() != nil {
		return
	}
	w.win.Update(func() {
		w.ready = r
		w.progress = p.Progress
		w.tasks = tasks
		w.downloads = downloads
		w.runProgress = rp
		w.loaded = true
		if e != nil {
			w.failure = human(e.Code)
		}
		if w.initDir == "" {
			w.initDir = text(r.Workspace.Path)
			if r.Workspace.Exists {
				w.initDir = fresh(w.initDir, "workspace")
			}
		}
		if candidate != w.candidate {
			w.binding = false
			w.process = ""
			w.candidate = candidate
			host, port := "", ""
			for i, a := range startup.Args {
				if i+1 < len(startup.Args) {
					if a == "--host" {
						host = startup.Args[i+1]
					}
					if a == "--port" {
						port = startup.Args[i+1]
					}
				}
			}
			if host != "" && port != "" {
				w.endpoint = "http://" + net.JoinHostPort(host, port)
			}
		}
		w.startup = startup
		w.processes = processes
	})
}
func (w *viewState) work(fn func() (string, *core.ApiError)) {
	if w.busy {
		return
	}
	w.busy = true
	w.failure = ""
	w.message = ""
	go func() {
		message, e := fn()
		if w.ctx.Err() != nil {
			return
		}
		w.win.Update(func() {
			w.busy = false
			if e != nil {
				w.failure = human(e.Code)
			} else {
				w.message = message
			}
		})
		w.refresh()
	}()
}
func (w *viewState) execute(command string, report bool) {
	w.work(func() (string, *core.ApiError) {
		r, e := w.service.WizardExecute(w.ctx, core.WizardRequest{Command: command, ReportVerify: report})
		if e != nil {
			return "", e
		}
		if r.ExitCode != 0 {
			return "", core.Err(string(r.Status))
		}
		return "操作完成，已更新工作区进度。", nil
	})
}
func (w *viewState) nextCommand() string {
	if w.progress == nil {
		return ""
	}
	b := w.active()
	command := text(w.progress.NextCommand)
	if b != nil && b.NextAction == "resume" {
		command = text(b.NextCommand)
	}
	args, e := inferyard.SplitCommand(command)
	if e != nil {
		return ""
	}
	// progress is the single source of truth: its next_command already picks a
	// fresh, non-existing --out via the CLI's new_output(); never rewrite it.
	if w.progress.NextAction == "prepare_candidate" {
		args = setArg(args, "--engine", w.engine)
	}
	if w.progress.NextAction == "device_check" && w.ready.Models.Path != nil {
		args = setArg(args, "--models-root", *w.ready.Models.Path)
	}
	if w.progress.NextAction == "bind" {
		pid, _, _ := strings.Cut(w.process, " · ")
		args = setArg(args, "--pid", pid)
		args = setArg(args, "--endpoint", w.endpoint)
	}
	return commandOf(args)
}
func setArg(args []string, flag, value string) []string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			args[i+1] = value
			return args
		}
	}
	return append(args, flag, value)
}
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func commandOf(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = quote(a)
	}
	return "inferyard " + strings.Join(out, " ")
}
func human(code string) string {
	switch code {
	case "unsupported_platform":
		return "当前平台不支持此候选流程，请检查平台支持范围。"
	case "device_preflight_blocked":
		return "设备检查未给出可运行推荐，请检查设备报告后重试。"
	case "blocked", "preflight_missing":
		return "设备检查尚未通过，请检查准备条件后重试。"
	case "incomplete", "run_incomplete":
		return "运行尚未封存，请检查停止原因后继续运行。"
	case "cancelled":
		return "操作已取消，可检查进度后重试。"
	case "output_exists":
		return "输出目录已存在，请选择新目录。"
	case "settings_incomplete":
		return "目录配置缺项，请先配置目录。"
	case "cli_unavailable":
		return "CLI 尚未就绪，请在主窗口设置路径。"
	case "invalid_request":
		return "输入不符合要求，请检查路径、URL 和环境变量名。"
	default:
		return "当前步骤未能完成，请检查主窗口诊断与工作区证据后重试。"
	}
}
