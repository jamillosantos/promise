package promise

import (
	"context"
)

type Resolve[T any] func(T)

type Reject func(error)

type Call[T any] func(context.Context) (T, error)

type Promise[T any] struct {
	state  state
	ch     chan struct{}
	result T
	err    error
}

// state represents the internal state of the promise.
type state int

const (
	// pending is the initial state of the promise before it starts.
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

// New creates a new promise that will be resolved when the function f completes.
//
// If f returns an error, the promise will be rejected.
//
// If f panics, the promise will be rejected with the panic error. If the panic is not an error, it will panic up.
//
// The given context is used to cancel the promise. However, the caller needs to make sure f is cancelable when the
// context is canceled. If f fails to be cancelable, the promise will be leaked until the promise is fulfilled or
// rejected.
func New[T any](ctx context.Context, f Call[T]) *Promise[T] {
	p := &Promise[T]{
		state: pending,
		ch:    make(chan struct{}),
	}
	go func() {
		defer func() {
			// This will capture a panic of an error and redirect it ot the reject.
			// If the panic recovered is not from an error, this function will panic up.
			close(p.ch)
			r := recover()
			if r == nil {
				return
			}
			switch d := r.(type) {
			case error:
				p.state = rejected
				p.err = d
			default:
				panic(d)
			}
		}()

		// Call function
		result, err := f(ctx) // The received ctx is the same as the one passed to f.
		if err != nil {
			p.state = rejected
			p.err = err
			return
		}
		p.state = fulfilled
		p.result = result
	}()
	return p
}

func Resolved[T any](v T) *Promise[T] {
	return &Promise[T]{
		state:  fulfilled,
		ch:     nil,
		result: v,
		err:    nil,
	}
}

func Rejected[T any](err error) *Promise[T] {
	return &Promise[T]{
		state: rejected,
		err:   err,
	}
}
