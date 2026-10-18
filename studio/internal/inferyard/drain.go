package inferyard

import (
	"context"
	"time"
)

type drainGraceKey struct{}

// WithDrainGrace preserves the frozen CLI idle wait plus bounded sealing overhead.
// It changes neither ordinary callers nor the actual process exit code.
func WithDrainGrace(ctx context.Context, grace time.Duration) context.Context {
	return context.WithValue(ctx, drainGraceKey{}, grace)
}
