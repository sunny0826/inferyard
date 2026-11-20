package pages

import "context"

type refreshContext struct {
	context.Context
	entered chan struct{}
}

func (c refreshContext) Err() error {
	c.entered <- struct{}{}
	return context.Canceled
}
