package promise

import (
	"context"
)

// Await blocks until p settles, ctx is done, or returns immediately if p is
// already settled.
//
// If p is already settled (including promises from Resolved/Rejected, whose
// internal channel is nil) Await returns its result/error right away without
// consulting ctx. When p is still pending, Await waits for whichever happens
// first: p settling or ctx being canceled.
//
// When p is settled and ctx is done at the same time, the settled result
// wins: a completed value is never discarded in favor of ctx.Err(). This
// holds both when both are ready on entry and when they become ready while
// Await is blocked.
func Await[T any](ctx context.Context, p *Promise[T]) (T, error) {
	if p.ch != nil {
		// Fast path: if the promise is already settled, take its result even
		// when ctx is also done, instead of letting select pick pseudo-randomly.
		select {
		case <-p.ch:
			// Promise already done.
		default:
			select {
			case <-p.ch:
				// Promise done.
			case <-ctx.Done():
				// ctx fired, but the promise may have settled in the same
				// instant: when both channels are ready, select picks one
				// pseudo-randomly. Re-check the settle channel so a completed
				// result is never discarded in favor of ctx.Err().
				select {
				case <-p.ch:
					// Settled concurrently; prefer the result.
				default:
					var empty T
					return empty, ctx.Err()
				}
			}
		}
	}

	switch p.loadState() {
	case fulfilled:
		return p.result, nil
	case rejected:
		var empty T
		return empty, p.err
	default:
		var empty T
		return empty, ErrInvalidState
	}
}
