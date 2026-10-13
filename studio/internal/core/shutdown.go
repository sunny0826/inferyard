package core

import "context"

// Shutdown closes admission immediately, drains cancelled runs, then reaps the
// owned engine. A caller timeout does not abandon cleanup or change its order.
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
			<-s.queueDone
			s.mu.Lock()
			e := s.engine
			if e != nil {
				s.stopEngineLocked(e)
			}
			s.mu.Unlock()
			if e != nil {
				<-e.done
			}
			close(s.shutdownDone)
		}()
	})
}

func (s *Service) admitRunLocked() *ApiError {
	if s.closing {
		return Err("service_closing")
	}
	if s.engine != nil && s.engine.state.State == "stopping" {
		return Err("engine_stopping")
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
