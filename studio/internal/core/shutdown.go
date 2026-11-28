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
		s.closeNodesOnShutdown()
		go func() {
			s.speedPreparing.Wait()
			<-s.queueDone
			s.mu.Lock()
			keepStandalone := EffectiveKeepServicesOnQuitValue(s.settings)
			if s.keepServicesOnShutdown != nil {
				keepStandalone = *s.keepServicesOnShutdown
			}
			var waitEntries []*engineEntry
			seenEntries := map[*engineEntry]bool{}
			handleEntry := func(e *engineEntry) {
				if e == nil || seenEntries[e] {
					return
				}
				seenEntries[e] = true
				if normalizeEngineOrigin(e.state.Origin) == EngineOriginStandalone &&
					keepStandalone &&
					(e.state.State == "starting" || e.state.State == "running") {
					s.detachEngineLocked(e)
				} else {
					s.stopEngineLocked(e)
				}
				waitEntries = append(waitEntries, e)
			}
			handleEntry(s.engine)
			for _, svc := range s.services {
				handleEntry(svc)
			}
			var orphanDones []<-chan struct{}
			if s.orphan != nil && s.orphan.state.State == "stopping" && s.orphan.done != nil {
				orphanDones = append(orphanDones, s.orphan.done)
			}
			for _, orphan := range s.orphanedServices {
				if orphan != nil && orphan.state.State == "stopping" && orphan.done != nil {
					orphanDones = append(orphanDones, orphan.done)
				}
			}
			s.mu.Unlock()
			for _, e := range waitEntries {
				<-e.done
			}
			for _, done := range orphanDones {
				<-done
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
	if s.hasActiveVenvUpgradeLocked() {
		return engineUpdatingError()
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
