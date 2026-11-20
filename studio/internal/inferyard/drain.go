package inferyard

import (
	"context"
	"time"
)

type drainGraceKey struct{}

func WithDrainGrace(ctx context.Context, grace time.Duration) context.Context {
	return context.WithValue(ctx, drainGraceKey{}, grace)
}
