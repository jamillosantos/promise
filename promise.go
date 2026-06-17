package promise

import (
	"context"
	"fmt"
	"sync/atomic"
)

type Resolve[T any] func(T)

type Reject func(error)

type Call[T any] func(context.Context) (T, error)

type Promise[T any] struct {
	// state holds the current state value. It is stored as an int32 so it can
	// be read and written atomically: the New goroutine publishes the final
	// state from one goroutine while callers (and tests) may observe it from
	// another. Use loadState to read it.
	state atomic.Int32
	// ch is closed exactly once when the promise settles. A receive on a closed
	// channel returns immediately, so Await uses it as the "done" signal. For
	// pre-settled promises (Resolved/Rejected) ch is nil; Await handles that.
	ch     chan struct{}
	result T
	err    error
}

// state represents the internal state of the promise.
type state int32

const (
	// pending is the initial state of the promise before it starts. It is the
	// zero value so a freshly created Promise is pending without explicit init.
	pending state = iota
	// fulfilled is the state of the promise when it completes successfully.
	fulfilled
	// rejected is the state of the promise when it completes with an error.
	rejected
)

func (s state) String() string {
	switch s {
	case pending:
		return "pending"
	case fulfilled:
		return "fulfilled"
	case rejected:
		return "rejected"
	default:
		return "unknown"
	}
}

// loadState atomically reads the current state of the promise.
func (p *Promise[T]) loadState() state {
	return state(p.state.Load())
}

// New creates a new promise that will be resolved when the function f completes.
//
// If f returns an error, the promise will be rejected.
//
// If f panics, the promise will be rejected: panics that are errors are used as
// the rejection error directly; any other panic value is wrapped in an error
// (fmt.Errorf("promise panicked: %v", v)). The panic never propagates out of the
// internal goroutine, so it cannot crash the host process.
//
// If f exits without returning at all — by calling runtime.Goexit, or by
// panicking with nil under the legacy GODEBUG=panicnil=1 semantics — the
// promise is rejected with ErrExitedWithoutResult.
//
// The given context is used to cancel the promise. However, the caller needs to make sure f is cancelable when the
// context is canceled. If f fails to be cancelable, the promise will be leaked until the promise is fulfilled or
// rejected.
func New[T any](ctx context.Context, f Call[T]) *Promise[T] {
	p := &Promise[T]{
		ch: make(chan struct{}),
	}
	// state defaults to pending (the atomic.Int32 zero value).
	go func() {
		// close(p.ch) is registered first, so by defer LIFO it runs LAST: after
		// every write to p.state/p.result/p.err on all exit paths (normal, error
		// and panic). That ordering gives any Await reader woken by <-p.ch a
		// happens-before edge on those writes, so it always observes the settled
		// state/result/err and never a stale pending.
		defer close(p.ch)
		defer func() {
			// Capture a panic from f and redirect it to a rejection. An error
			// panic is used as-is; any other value is wrapped so the failure is
			// delivered to Await instead of crashing the process.
			if r := recover(); r != nil {
				switch d := r.(type) {
				case error:
					p.err = d
				default:
					p.err = fmt.Errorf("promise panicked: %v", d)
				}
				p.state.Store(int32(rejected))
				return
			}
			// recover() returned nil but the promise never settled: f exited
			// without returning. This happens when f calls runtime.Goexit, or
			// panics with nil under the legacy GODEBUG=panicnil=1 semantics
			// (where recover returns nil for panic(nil)). Without this branch
			// the channel would close with the state still pending and Await
			// would surface the misleading ErrInvalidState.
			if p.loadState() == pending {
				p.err = ErrExitedWithoutResult
				p.state.Store(int32(rejected))
			}
		}()

		// Call function. The received ctx is the same as the one passed to f.
		result, err := f(ctx)
		if err != nil {
			p.err = err
			p.state.Store(int32(rejected))
			return
		}
		p.result = result
		p.state.Store(int32(fulfilled))
	}()
	return p
}

// Resolved returns an already-fulfilled promise holding v. Await on it returns
// immediately. It spawns no goroutine.
func Resolved[T any](v T) *Promise[T] {
	p := &Promise[T]{
		result: v,
	}
	p.state.Store(int32(fulfilled))
	return p
}

// Rejected returns an already-rejected promise holding err. Await on it returns
// immediately. It spawns no goroutine.
func Rejected[T any](err error) *Promise[T] {
	p := &Promise[T]{
		err: err,
	}
	p.state.Store(int32(rejected))
	return p
}
