//go:build studio_dev && !release

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/desktop"
	"inferyard/studio/internal/pages"
)

func registerDevelopment(flags *flag.FlagSet, o *startupOptions) {
	flags.StringVar(&o.devManifest, "dev-manifest", "", "Launch the paired checkout development environment")
	flags.StringVar(&o.devCheck, "dev-check", "", "Validate a development manifest without opening a window")
	flags.StringVar(&o.devStop, "dev-stop", "", "Request normal shutdown of a verified development session")
	flags.StringVar(&o.devRecoveryController, "dev-recovery-controller", "", "Verify a missing-session target with this prepared controller")
	flags.StringVar(&o.devRecoveryTarget, "dev-recovery-target", "", "Original launch manifest to verify for recovery")
}

func runDevelopment(ctx context.Context, cancel context.CancelFunc, o startupOptions) (bool, error) {
	if o.devRecoveryController != "" {
		controller, err := loadDevManifest(o.devRecoveryController)
		if err != nil {
			return true, err
		}
		if err := controller.checkSelf(); err != nil {
			return true, err
		}
		target, err := loadDevManifest(o.devRecoveryTarget)
		if err != nil {
			return true, err
		}
		result, err := recoverDevelopment(ctx, controller, target, nativeDevRecoveryReader())
		if err != nil {
			return true, err
		}
		return true, json.NewEncoder(os.Stdout).Encode(result)
	}
	path := o.devManifest
	if o.devCheck != "" {
		path = o.devCheck
	}
	if o.devStop != "" {
		path = o.devStop
	}
	if path == "" {
		if o.debugUI {
			return false, nil
		}
		return true, fmt.Errorf("development builds require a launch manifest; use mise run dev")
	}
	m, err := loadDevManifest(path)
	if err != nil {
		return true, err
	}
	if err := m.checkSelf(); err != nil {
		return true, err
	}
	if o.devStop != "" {
		return true, m.stopSession(ctx, nativeDevProcessReader())
	}
	if o.devCheck != "" {
		return true, m.checkCLI(ctx)
	}
	return true, m.runSession(ctx, cancel, func(session *devSession) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return launchDevelopment(ctx, cancel, m, session)
	})
}

func launchDevelopment(ctx context.Context, cancel context.CancelFunc, m *devManifest, session *devSession) (result error) {
	service, err := core.New(ctx, core.Options{Paths: m.boundPaths(), ValidateCLI: m.checkCLI})
	if err != nil {
		return err
	}
	ready := false
	defer func() {
		cancel()
		shutdownCtx, stop := context.WithTimeout(context.Background(), 45*time.Second)
		defer stop()
		err := service.Shutdown(shutdownCtx)
		if err != nil {
			session.State = "shutdown_failed"
			result = err
		}
	}()
	probe := service.Status().Inferyard
	if probe.Status != "ok" || probe.Path == nil || *probe.Path != m.cliPath() {
		return fmt.Errorf("Service CLI probe did not match manifest")
	}
	session.CLIPath = *probe.Path
	if err := ctx.Err(); err != nil {
		return err
	}
	mygo.App.SetWindowStateStore(devWindowStates{filepath.Join(m.ProfileRoot, "window-state.json")})
	runApp(ctx, func() {
		if ctx.Err() != nil {
			mygo.App.Quit()
			return
		}
		desktop.Subscribe(ctx, service.Bus, func(message desktop.Message) error {
			return mygo.NewNotification(mygo.NotificationOptions{Title: message.Title, Body: message.Body}).Show()
		}, log.Printf)
		pages.New(ctx, service, "")
		session.State = "ready"
		if err := atomicDevJSON(m.sessionPath(), session); err != nil {
			result = err
			cancel()
			return
		}
		ready = true
	})
	if !ready && result == nil {
		result = fmt.Errorf("development window did not become ready")
	}
	return result
}

type devWindowStates struct{ path string }

func (s devWindowStates) LoadWindowStates() (map[string]mygo.WindowState, error) {
	if err := canonicalDevPath(s.path); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var states map[string]mygo.WindowState
	err = json.Unmarshal(raw, &states)
	return states, err
}
func (s devWindowStates) SaveWindowStates(states map[string]mygo.WindowState) error {
	return atomicDevJSON(s.path, states)
}
