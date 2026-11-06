package pages

import "context"

// Record refresh entry and stop before any backend or native-window work.
type refreshContext struct {
	context.Context
	entered chan struct{}
}

func (c refreshContext) Err() error {
	c.entered <- struct{}{}
	return context.Canceled
}
