package core

import "context"

// SetKeepServicesOnShutdown overrides whether standalone services stay running during Shutdown.
func (s *Service) SetKeepServicesOnShutdown(keep bool) {
	s.mu.Lock()
	s.keepServicesOnShutdown = ptr(keep)
	s.mu.Unlock()
}

// ShutdownWithOptions shuts down the service while explicitly choosing whether to stop standalone services.
func (s *Service) ShutdownWithOptions(ctx context.Context, stopServices bool) error {
	s.SetKeepServicesOnShutdown(!stopServices)
	return s.Shutdown(ctx)
}

func (s *Service) Shutdown(ctx context.Context) error {
	s.beginShutdown()
	select {
	case <-s.shutdownDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) beginShutdown() {
	s.shutdownOnce.Do(func() {
		s.mu.Lock()
		s.closing = true
		for _, t := range s.runs {
			if terminal(t.Task.State) {
				continue
			}
			if t.Task.Queued {
				t.Task.State, t.Task.Queued = "cancelled", false
				t.Task.FinishedAt = ptr(now())
			} else {
				t.Task.State = "cancelling"
			}
			s.state(t)
		}
		s.cancel()
		s.mu.Unlock()
		go func() {
			s.speedPreparing.Wait()
			<-s.queueDone
			s.mu.Lock()
			e := s.engine
			keepStandalone := EffectiveKeepServicesOnQuitValue(s.settings)
			if s.keepServicesOnShutdown != nil {
				keepStandalone = *s.keepServicesOnShutdown
			}
			var orphanDone <-chan struct{}
			if s.orphan != nil && s.orphan.state.State == "stopping" {
				orphanDone = s.orphan.done
			}
			if e != nil {
				if normalizeEngineOrigin(e.state.Origin) == EngineOriginStandalone &&
					keepStandalone &&
					(e.state.State == "starting" || e.state.State == "running") {
					s.detachEngineLocked(e)
				} else {
					s.stopEngineLocked(e)
				}
			}
			s.mu.Unlock()
			if e != nil {
				<-e.done
			}
			if orphanDone != nil {
				<-orphanDone
			}
			s.cleanupUpdateStagingOnShutdown()
			close(s.shutdownDone)
		}()
	})
}

func (s *Service) admitRunLocked() *ApiError {
	if s.closing {
		return Err("service_closing")
	}
	if s.updateApplying {
		return Conflict("update_applying")
	}
	if s.deletingAssets > 0 {
		return workspaceBusyError()
	}
	for _, t := range s.runs {
		if t.Task.ContextSpeed != nil && !terminal(t.Task.State) {
			return Conflict("context_speed_preparing")
		}
	}
	if (s.engine != nil && s.engine.state.State == "stopping") ||
		(s.orphan != nil && s.orphan.state.State == "stopping") {
		return Err("engine_stopping")
	}
	return nil
}

func (s *Service) admitDownloadLocked() *ApiError {
	if s.closing {
		return Err("service_closing")
	}
	if s.updateApplying {
		return Conflict("update_applying")
	}
	if s.deletingAssets > 0 {
		return workspaceBusyError()
	}
	return nil
}

func (s *Service) runsActiveLocked() bool {
	for _, t := range s.runs {
		if !terminal(t.Task.State) {
			return true
		}
	}
	return false
}
