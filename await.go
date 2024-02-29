package promise

import (
	"context"
)

func Await[T any](ctx context.Context, p *Promise[T]) (T, error) {
	select {
	case <-p.ch:
		// Promise done
	case <-ctx.Done():
		// Promise cancelled
		var empty T
		return empty, ctx.Err()
	}
	switch p.state {
	case fulfilled:
		return p.result, nil
	case rejected:
		return p.result, p.err
	default:
		return p.result, ErrInvalidState
	}
}
